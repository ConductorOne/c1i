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

var policiesUpdateCmd = &cobra.Command{
	Use:   "update <policy-id>",
	Short: "Update a policy (pretty JSON)",
	Long: `Update a policy. The API requires the request wrapped as
{"policy": {...}, "updateMask": "..."} — a flat/unwrapped policy body 400s
(protojson rejects its top-level keys as unknown fields on
UpdatePolicyRequest). This command builds that wrapper for you; you never
need to construct it yourself.

The update_mask is derived from which flags/files you pass — e.g.
--display-name alone masks only "displayName". Pass --steps-file /
--rules-file the same way "create" takes them (see its --help for the file
shape); each REPLACES its entire field, not a merge into the existing one.
A blank --display-name (empty, whitespace or invisible-only) exits 2,
nothing sent. Sent as is, the server stores "" as an empty name and keeps
"  " verbatim.

Only one of --body-file, --steps-file and --rules-file can read stdin ("-");
two exit 2 before reading.

For anything the convenience flags can't express, pass the full Policy
object yourself via --body-file plus an explicit --update-mask (both
required together, mutually exclusive with the convenience flags). Mask
paths are comma-separated; spaces around them are trimmed, and an empty path
or an --update-mask without --body-file exits 2.

The same client-side guards as "create" run here too (exit code 2 before
any request is sent) — including the empty-steps guard, which also
applies to update: clearing a policy's baseline steps entry (or replacing
it with an empty array) hits the exact same silent-deny-all default /
HTTP-500 crash as create does. --allow-deny-all bypasses the deny-all case
(not the crash case, which is never safe to bypass).

Some of those guards (e.g. agent steps only in grant/certify) depend on
policyType. If your patch changes policySteps but doesn't state policyType
(--steps-file without --policy-type, or a --body-file body that omits it),
this command fetches the policy's current type first so those guards can
still run. A failed lookup (e.g. the policy doesn't exist, or you're not
authorized) surfaces as that failure's own exit code, not exit 2; if the
lookup succeeds but still can't produce a usable type, the command refuses
with exit 2 rather than send an unguarded request. Pass --policy-type to
skip the lookup; --dry-run skips it too, so it needs --policy-type to preview
such a patch (exit 2 otherwise).

Prints the updated policy unwrapped like "policies get" (id is .id).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		m := mutation{Method: http.MethodPost, Path: client.Path("/api/v1/policies/%s", id)}
		// Read once: both passes below build from these, and stdin drains.
		in, err := readPolicyUpdateInputs(cmd)
		if err != nil {
			return err
		}

		// A first pass with a resolver that only reports "type needed" builds
		// the body offline whenever the patch states its own policyType.
		body, err := buildPolicyUpdateBody(cmd, id, in, func() (string, error) { return "", errPolicyTypeNeeded })
		if err == nil {
			m.Body = body
			return sendMutation(cmd, m)
		}
		if !errors.Is(err, errPolicyTypeNeeded) {
			return err
		}
		if dryRunActive() {
			return &usageError{errors.New("--dry-run skips the lookup of the policy's current type, which this patch needs: pass --policy-type")}
		}
		return sendMutation(cmd, m, withPreRead(preRead{Run: func(ctx context.Context, c *client.Client, m *mutation) ([]resolvedID, error) {
			r := &policyTypeResolver{client: c}
			body, err := buildPolicyUpdateBody(cmd, id, in, func() (string, error) { return r.resolve(ctx, id) })
			m.Body = body
			return nil, err
		}}))
	},
}

var errPolicyTypeNeeded = errors.New("policy type lookup needed")

// policyBodyExclusive are the flags --body-file cannot be combined with.
var policyBodyExclusive = []string{"display-name", "description", "policy-type", "steps-file", "rules-file"}

// policyUpdateInputs holds the --*-file contents, read once per run.
type policyUpdateInputs struct {
	body         map[string]any
	steps, rules []any
}

func readPolicyUpdateInputs(cmd *cobra.Command) (policyUpdateInputs, error) {
	var in policyUpdateInputs
	// Validate flags before any read, so a bad mask never drains stdin.
	if _, _, err := bodyFileMask(cmd, policyBodyExclusive...); err != nil {
		return in, err
	}
	if err := refuseMultipleStdin(cmd); err != nil {
		return in, err
	}
	var err error
	if cmd.Flags().Changed("body-file") {
		f, _ := cmd.Flags().GetString("body-file")
		if in.body, err = readJSONFile[map[string]any](cmd, "body-file", f); err != nil {
			return in, err
		}
	}
	if cmd.Flags().Changed("steps-file") {
		if in.steps, err = readJSONFile[[]any](cmd, "steps-file", mustGetString(cmd, "steps-file")); err != nil {
			return in, err
		}
	}
	if cmd.Flags().Changed("rules-file") {
		if in.rules, err = readJSONFile[[]any](cmd, "rules-file", mustGetString(cmd, "rules-file")); err != nil {
			return in, err
		}
	}
	return in, nil
}

// buildPolicyUpdateBody builds and guards the wrapped update body. resolve
// supplies the policy's current type when the patch carries policySteps
// without one of its own.
func buildPolicyUpdateBody(cmd *cobra.Command, id string, in policyUpdateInputs, resolve func() (string, error)) (map[string]any, error) {
	allowDenyAll, _ := cmd.Flags().GetBool("allow-deny-all")
	policy, mask, err := buildUpdatePolicyPatch(cmd, in, resolve)
	if err != nil {
		// A classified fetch failure keeps its exit code; anything else is usage.
		var apiErr *client.APIError
		var authErr *client.AuthError
		if errors.As(err, &apiErr) || errors.As(err, &authErr) {
			return nil, err
		}
		return nil, &usageError{err}
	}

	effectivePolicyType, _ := policy["policyType"].(string)
	if effectivePolicyType == "" {
		if _, hasSteps := policy["policySteps"].(map[string]any); hasSteps {
			effectivePolicyType, err = resolve()
			if err != nil {
				return nil, err
			}
			if effectivePolicyType == "" {
				return nil, &usageError{fmt.Errorf("this update sets policySteps but no policyType was given and the existing policy at %q has no resolvable type — pass --policy-type explicitly so the policyType-dependent guards (and the server) know what they're validating", id)}
			}
		}
	}
	if err := validateUpdatePatch(policy, effectivePolicyType, allowDenyAll); err != nil {
		return nil, err
	}

	policy["id"] = id
	return map[string]any{"policy": policy, "updateMask": mask}, nil
}

// policyTypeResolver memoizes a policy's current type, failure included, so a
// second caller sees the same classified error instead of an empty type that
// would downgrade a real 401/404 to exit 2.
type policyTypeResolver struct {
	client *client.Client
	value  string
	err    error
	done   bool
}

func (r *policyTypeResolver) resolve(ctx context.Context, id string) (string, error) {
	if !r.done {
		r.done = true
		r.value, r.err = fetchPolicyType(ctx, r.client, id)
	}
	return r.value, r.err
}

// fetchPolicyType looks up an existing policy's policyType.
func fetchPolicyType(ctx context.Context, c *client.Client, id string) (string, error) {
	meta, err := fetchPolicyMeta(ctx, c, id)
	if err != nil {
		return "", fmt.Errorf("resolving the policy's current type (pass --policy-type to skip this lookup): %w", err)
	}
	return meta.policyType, nil
}

// buildUpdatePolicyPatch builds the partial "policy" object and its
// update_mask from flags and in, or from a copy of in.body plus --update-mask.
// It does no I/O itself; resolveFallbackPolicyType supplies the baseline key
// when --steps-file comes without --policy-type. "id" is left to the caller,
// so the guards see only the fields being changed.
func buildUpdatePolicyPatch(cmd *cobra.Command, in policyUpdateInputs, resolveFallbackPolicyType func() (string, error)) (policy map[string]any, mask string, err error) {
	mask, usingBody, err := bodyFileMask(cmd, policyBodyExclusive...)
	if err != nil {
		return nil, "", err
	}

	if usingBody {
		// A copy: the caller sets "id" on it, and a second pass reuses in.
		policy = make(map[string]any, len(in.body))
		for k, v := range in.body {
			policy[k] = v
		}
		return policy, mask, nil
	}

	policy = map[string]any{}
	var paths []string

	if cmd.Flags().Changed("display-name") {
		v, _ := cmd.Flags().GetString("display-name")
		policy["displayName"] = v
		paths = append(paths, "displayName")
	}
	if cmd.Flags().Changed("description") {
		v, _ := cmd.Flags().GetString("description")
		policy["description"] = v
		paths = append(paths, "description")
	}
	if cmd.Flags().Changed("policy-type") {
		v, err := policyTypeFlag.parse(mustGetString(cmd, "policy-type"))
		if err != nil {
			return nil, "", err
		}
		policy["policyType"] = v
		paths = append(paths, "policyType")
	}
	if cmd.Flags().Changed("steps-file") {
		steps := in.steps
		policyType, _ := policy["policyType"].(string)
		if policyType == "" {
			policyType, err = resolveFallbackPolicyType()
			if err != nil {
				return nil, "", err
			}
		}
		key, keyErr := policyStepsKey(policyType)
		if keyErr != nil {
			return nil, "", keyErr
		}
		policy["policySteps"] = map[string]any{key: map[string]any{"steps": steps}}
		paths = append(paths, "policySteps")
	}
	if cmd.Flags().Changed("rules-file") {
		policy["rules"] = in.rules
		paths = append(paths, "rules")
	}
	if len(paths) == 0 {
		return nil, "", fmt.Errorf("nothing to update: pass --display-name, --description, --policy-type, --steps-file, --rules-file, or --body-file with --update-mask")
	}

	return policy, strings.Join(paths, ","), nil
}

func init() {
	policiesUpdateCmd.Flags().String("display-name", "", "New display name (omit to leave unchanged; "+displayNameRule+")")
	policiesUpdateCmd.Flags().String("description", "", "New description")
	allowEmpty(policiesUpdateCmd, emptyIsSent, "description")
	policiesUpdateCmd.Flags().String("policy-type", "", "New policy type: "+policyTypeFlag.usage()+" (rarely changed)")
	policiesUpdateCmd.Flags().String("steps-file", "", "JSON array: replaces the baseline policySteps.steps content (file, or \"-\" for stdin)")
	policiesUpdateCmd.Flags().String("rules-file", "", "JSON array: replaces rules[] (file, or \"-\" for stdin)")
	policiesUpdateCmd.Flags().String("body-file", "", "Full Policy JSON object, verbatim (file, or \"-\" for stdin); requires --update-mask; mutually exclusive with the flags above")
	policiesUpdateCmd.Flags().String("update-mask", "", bodyFileMaskUsage)
	setBlankHint(policiesUpdateCmd, "update-mask", updateMaskBlankHint)
	policiesUpdateCmd.Flags().Bool("allow-deny-all", false, "Allow leaving/setting a policy with no steps — bypasses the empty-steps guard on purpose")
	markMutating(policiesUpdateCmd)
	policiesCmd.AddCommand(policiesUpdateCmd)
}
