package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// policyKind is one bindable policy slot on a resource: the --unbind-policy
// kind, the <kind>-policy-id flag, the JSON field, and the policyType the id
// must name.
type policyKind struct{ kind, flag, field, wantType string }

func newPolicyKind(kind, field, wantType string) policyKind {
	return policyKind{kind: kind, flag: kind + "-policy-id", field: field, wantType: wantType}
}

// appPolicyKinds are the App's policy slots, in mask order. Another resource
// declares its own table (e.g. adding an emergency-grant slot).
var appPolicyKinds = []policyKind{
	newPolicyKind("grant", "grantPolicyId", "POLICY_TYPE_GRANT"),
	newPolicyKind("revoke", "revokePolicyId", "POLICY_TYPE_REVOKE"),
	newPolicyKind("certify", "certifyPolicyId", "POLICY_TYPE_CERTIFY"),
}

// entitlementPolicyKinds adds the emergency slot, which takes a grant policy.
// The server checks that type only when the same request enables emergency
// grants, so it is checked here on every bind.
var entitlementPolicyKinds = append(append([]policyKind(nil), appPolicyKinds...),
	newPolicyKind("emergency-grant", "emergencyGrantPolicyId", "POLICY_TYPE_GRANT"))

func registerPolicyIDFlags(cmd *cobra.Command, kinds []policyKind, suffix string) {
	for _, k := range kinds {
		typ := strings.ToLower(strings.TrimPrefix(k.wantType, "POLICY_TYPE_"))
		cmd.Flags().String(k.flag, "", fmt.Sprintf("%s policy ID to bind; must exist and be a %s policy%s", strings.ToUpper(k.kind[:1])+k.kind[1:], typ, suffix))
	}
}

// unbindPolicyFlag builds the --unbind-policy table from a resource's kinds.
func unbindPolicyFlag(kinds []policyKind) enumFlag {
	e := enumFlag{flag: "unbind-policy", unknown: enumReject}
	for _, k := range kinds {
		e.values = append(e.values, enumValue{name: k.kind, api: k.kind})
	}
	return e
}

func registerUnbindPolicyFlag(cmd *cobra.Command, kinds []policyKind) {
	addRepeatableStringFlag(cmd, "unbind-policy", "Unbind a policy: "+unbindPolicyFlag(kinds).usage()+" (repeatable)")
}

// policyIDFlag reads a policy-id flag; a passed-but-blank value is a usage
// error, since an unset shell variable would otherwise unbind the policy.
func policyIDFlag(cmd *cobra.Command, k policyKind) (string, bool, error) {
	if !cmd.Flags().Changed(k.flag) {
		return "", false, nil
	}
	v, _ := cmd.Flags().GetString(k.flag)
	v = strings.TrimSpace(v)
	if v == "" {
		msg := fmt.Sprintf("--%s requires a non-empty value", k.flag)
		if cmd.Flags().Lookup("unbind-policy") != nil {
			msg += "; to unbind, use --unbind-policy " + k.kind
		}
		return "", false, &usageError{errors.New(msg)}
	}
	return v, true, nil
}

// applyPolicyIDs sets each passed policy-id flag on obj (create: no unbind).
func applyPolicyIDs(cmd *cobra.Command, kinds []policyKind, obj map[string]any) error {
	for _, k := range kinds {
		v, ok, err := policyIDFlag(cmd, k)
		if err != nil {
			return err
		}
		if ok {
			obj[k.field] = v
		}
	}
	return nil
}

// applyPolicyUpdates sets or unbinds each policy slot on obj and returns the
// mask paths, in kind order.
func applyPolicyUpdates(cmd *cobra.Command, kinds []policyKind, obj map[string]any) ([]string, error) {
	raw, err := parseRepeatableEnum(cmd, unbindPolicyFlag(kinds))
	if err != nil {
		return nil, err
	}
	unbind := map[string]bool{}
	for _, v := range raw {
		unbind[v] = true
	}
	var paths []string
	for _, k := range kinds {
		v, set, err := policyIDFlag(cmd, k)
		if err != nil {
			return nil, err
		}
		switch {
		case set && unbind[k.kind]:
			return nil, &usageError{fmt.Errorf("--%s and --unbind-policy %s are mutually exclusive", k.flag, k.kind)}
		case set:
			obj[k.field] = v
		case unbind[k.kind]:
			obj[k.field] = ""
		default:
			continue
		}
		paths = append(paths, k.field)
	}
	return paths, nil
}

// policyBindingsCheck is validatePolicyBindings as a mutation's pre-send read;
// a dry run skips it.
func policyBindingsCheck(cmd *cobra.Command, kinds []policyKind) preRead {
	return preRead{Run: func(ctx context.Context, c *client.Client, _ *mutation) ([]resolvedID, error) {
		return nil, validatePolicyBindings(ctx, cmd, c, kinds)
	}}
}

// validatePolicyBindings checks that every policy id being bound names an
// existing, undeleted policy of the slot's type. Each distinct id is fetched
// once; an unbind is never looked up. Non-404 failures keep their own class.
func validatePolicyBindings(ctx context.Context, cmd *cobra.Command, c *client.Client, kinds []policyKind) error {
	types := map[string]string{}
	for _, k := range kinds {
		id, set, err := policyIDFlag(cmd, k)
		if err != nil {
			return err
		}
		if !set {
			continue
		}
		got, seen := types[id]
		if !seen {
			if got, err = fetchBoundPolicyType(ctx, c, k.flag, id); err != nil {
				return err
			}
			types[id] = got
		}
		if got != k.wantType {
			return &usageError{fmt.Errorf("--%s %q is a %s policy; it must be %s", k.flag, id, got, k.wantType)}
		}
	}
	return nil
}

func fetchBoundPolicyType(ctx context.Context, c *client.Client, flag, id string) (string, error) {
	meta, err := fetchPolicyMeta(ctx, c, id)
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return "", &usageError{fmt.Errorf("--%s %q: no such policy", flag, id)}
		}
		return "", fmt.Errorf("checking --%s %q: %w", flag, id, err)
	}
	if !meta.found || meta.policyType == "" {
		return "", &nonJSONResponseError{fmt.Errorf("checking --%s %q: the response held no policy with a policyType", flag, id)}
	}
	// GET still returns a soft-deleted policy.
	if meta.deleted {
		return "", &usageError{fmt.Errorf("--%s %q: policy is deleted", flag, id)}
	}
	return meta.policyType, nil
}
