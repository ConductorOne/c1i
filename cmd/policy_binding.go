package cmd

import (
	"context"
	"encoding/json"
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

func registerPolicyIDFlags(cmd *cobra.Command, kinds []policyKind, suffix string) {
	for _, k := range kinds {
		cmd.Flags().String(k.flag, "", fmt.Sprintf("%s policy ID to bind; must exist and be a %s policy%s", strings.ToUpper(k.kind[:1])+k.kind[1:], k.kind, suffix))
	}
}

func registerUnbindPolicyFlag(cmd *cobra.Command, kinds []policyKind) {
	names := make([]string, len(kinds))
	for i, k := range kinds {
		names[i] = k.kind
	}
	addRepeatableStringFlag(cmd, "unbind-policy", "Unbind a policy: "+strings.Join(names, ", ")+" (repeatable)")
}

// policyIDFlag reads a policy-id flag; a passed-but-blank value is a usage
// error, since an unset shell variable would otherwise unbind the policy.
func policyIDFlag(cmd *cobra.Command, k policyKind) (string, bool, error) {
	if !cmd.Flags().Changed(k.flag) {
		return "", false, nil
	}
	v, _ := cmd.Flags().GetString(k.flag)
	if strings.TrimSpace(v) == "" {
		return "", false, &usageError{fmt.Errorf("--%s requires a non-empty value; to unbind, use --unbind-policy %s", k.flag, k.kind)}
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
	raw, err := repeatableStringFlag(cmd, "unbind-policy")
	if err != nil {
		return nil, err
	}
	unbind := map[string]bool{}
	for _, r := range raw {
		v := strings.ToLower(strings.TrimSpace(r))
		known := false
		for _, k := range kinds {
			known = known || k.kind == v
		}
		if !known {
			names := make([]string, len(kinds))
			for i, k := range kinds {
				names[i] = k.kind
			}
			return nil, &usageError{fmt.Errorf("invalid --unbind-policy %q: want %s", r, strings.Join(names, ", "))}
		}
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
	data, err := c.Get(ctx, client.Path("/api/v1/policies/%s", id), nil)
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return "", &usageError{fmt.Errorf("--%s %q: no such policy", flag, id)}
		}
		return "", fmt.Errorf("checking --%s %q: %w", flag, id, err)
	}
	var resp struct {
		Policy struct {
			PolicyType string  `json:"policyType"`
			DeletedAt  *string `json:"deletedAt"`
		} `json:"policy"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("parsing policy %q: %w", id, err)
	}
	// GET still returns a soft-deleted policy.
	if resp.Policy.DeletedAt != nil {
		return "", &usageError{fmt.Errorf("--%s %q: policy is deleted", flag, id)}
	}
	return resp.Policy.PolicyType, nil
}
