package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
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

For anything the convenience flags can't express, pass the full Policy
object yourself via --body-file plus an explicit --update-mask (both
required together) — mutually exclusive with the convenience flags.

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
skip the lookup.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		allowDenyAll, _ := cmd.Flags().GetBool("allow-deny-all")

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		// Lazily fetches the existing policy's type, whenever the patch ends
		// up carrying policySteps without an explicit policyType of its own
		// (needed both to resolve --steps-file's baseline map key, and to
		// give the policyType-dependent guards below something to check —
		// omitting it let an update slip an agent step past every one of
		// them straight through to the server). Memoized so a plain
		// display-name/description update never pays for it, and a
		// policySteps-carrying update only pays for it once regardless of
		// which flag/file path produced the patch.
		resolver := &policyTypeResolver{baseURL: baseURL}

		policy, mask, err := buildUpdatePolicyPatch(cmd, func() (string, error) { return resolver.resolve(cmd, id) })
		if err != nil {
			// A fetch failure (auth/not-found/rate-limited/server) reaches
			// here already classified — surface it as-is so exitCode can map
			// it correctly (3/4/5/6), rather than flattening it to exitUsage.
			// Anything else from buildUpdatePolicyPatch (bad flag combo,
			// unreadable/unparsable file, an unmappable --policy-type) is a
			// genuine usage problem and becomes exitUsage as before.
			var apiErr *client.APIError
			var authErr *client.AuthError
			if errors.As(err, &apiErr) || errors.As(err, &authErr) {
				return err
			}
			return &usageError{err}
		}

		effectivePolicyType, _ := policy["policyType"].(string)
		if effectivePolicyType == "" {
			if _, hasSteps := policy["policySteps"].(map[string]any); hasSteps {
				effectivePolicyType, err = resolver.resolve(cmd, id)
				if err != nil {
					return err // classified fetch failure — do not wrap in usageError
				}
				if effectivePolicyType == "" {
					return &usageError{fmt.Errorf("this update sets policySteps but no policyType was given and the existing policy at %q has no resolvable type — pass --policy-type explicitly so the policyType-dependent guards (and the server) know what they're validating", id)}
				}
			}
		}
		if err := validateUpdatePatch(policy, effectivePolicyType, allowDenyAll); err != nil {
			return err
		}

		policy["id"] = id
		body := map[string]any{"policy": policy, "updateMask": mask}

		path := client.Path("/api/v1/policies/%s", id)
		if dryRunActive() {
			return printDryRun(cmd, "POST", path, body)
		}

		c := resolver.client
		if c == nil {
			c, err = newPoliciesClient(cmd, baseURL)
			if err != nil {
				return fmt.Errorf("authentication failed: %w", err)
			}
		}
		data, err := c.Post(cmd.Context(), path, body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeRawObject(cmd, data)
	},
}

// policyTypeResolver lazily fetches and memoizes a policy's current type
// (and client) across the several call sites in RunE and
// buildUpdatePolicyPatch that may need it for the same update. A failed
// fetch is memoized alongside the value, so a second caller sees the same
// classified error rather than an empty type that would look like "resolved
// to nothing" and downgrade a real 401/404 to exit 2. Not safe for
// concurrent use; RunE calls it sequentially.
type policyTypeResolver struct {
	baseURL string
	client  *client.Client
	value   string
	err     error
	done    bool
}

// resolve returns the policy's type, fetching and memoizing it (value and
// error alike) on the first call.
func (r *policyTypeResolver) resolve(cmd *cobra.Command, id string) (string, error) {
	if r.done {
		return r.value, r.err
	}
	r.done = true
	if r.client == nil {
		c, err := newPoliciesClient(cmd, r.baseURL)
		if err != nil {
			r.err = fmt.Errorf("authentication failed: %w", err)
			return "", r.err
		}
		r.client = c
	}
	r.value, r.err = fetchPolicyType(cmd, r.client, id)
	return r.value, r.err
}

// fetchPolicyType looks up an existing policy's policyType. Called via
// policyTypeResolver.resolve whenever an update's patch carries policySteps
// without its own policyType, regardless of whether that patch came from
// --steps-file or --body-file.
func fetchPolicyType(cmd *cobra.Command, c *client.Client, id string) (string, error) {
	data, err := c.Get(cmd.Context(), client.Path("/api/v1/policies/%s", id), nil)
	if err != nil {
		return "", fmt.Errorf("resolving the policy's current type (pass --policy-type to skip this lookup): %w", err)
	}
	var resp struct {
		Policy struct {
			PolicyType string `json:"policyType"`
		} `json:"policy"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("parsing policy response: %w", err)
	}
	return resp.Policy.PolicyType, nil
}

// buildUpdatePolicyPatch builds the partial "policy" object and its
// update_mask from flags, or reads both (policy verbatim, mask from
// --update-mask) when --body-file is used. Pure aside from
// resolveFallbackPolicyType, which is only invoked (and only does network
// I/O) here when --steps-file is supplied without --policy-type, to resolve
// the policySteps baseline map key — a --body-file patch needs the same
// fallback for the policyType-dependent guards, but can't resolve it here
// (its policySteps, if any, is opaque JSON with no map key to derive), so
// the caller (RunE) resolves it again afterward when needed; the closure is
// memoized, so that costs nothing extra. The "id" field is intentionally
// NOT set here — the caller adds it after guard validation, matching
// create's shape where the guards see exactly the fields being changed.
func buildUpdatePolicyPatch(cmd *cobra.Command, resolveFallbackPolicyType func() (string, error)) (policy map[string]any, mask string, err error) {
	bodyFile, _ := cmd.Flags().GetString("body-file")
	usingFlags := cmd.Flags().Changed("display-name") || cmd.Flags().Changed("description") ||
		cmd.Flags().Changed("policy-type") || cmd.Flags().Changed("steps-file") || cmd.Flags().Changed("rules-file")

	if bodyFile != "" {
		if usingFlags {
			return nil, "", fmt.Errorf("--body-file is mutually exclusive with --display-name/--description/--policy-type/--steps-file/--rules-file")
		}
		mask, _ = cmd.Flags().GetString("update-mask")
		if mask == "" {
			return nil, "", fmt.Errorf("--update-mask is required together with --body-file")
		}
		policy, err = readConfigFile(cmd, bodyFile)
		if err != nil {
			return nil, "", err
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
		v, _ := cmd.Flags().GetString("policy-type")
		policy["policyType"] = mapPolicyType(v)
		paths = append(paths, "policyType")
	}
	if cmd.Flags().Changed("steps-file") {
		stepsFile, _ := cmd.Flags().GetString("steps-file")
		steps, stepsErr := readJSONArrayFile(cmd, stepsFile)
		if stepsErr != nil {
			return nil, "", fmt.Errorf("reading --steps-file: %w", stepsErr)
		}
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
		rulesFile, _ := cmd.Flags().GetString("rules-file")
		rules, rulesErr := readJSONArrayFile(cmd, rulesFile)
		if rulesErr != nil {
			return nil, "", fmt.Errorf("reading --rules-file: %w", rulesErr)
		}
		policy["rules"] = rules
		paths = append(paths, "rules")
	}
	if len(paths) == 0 {
		return nil, "", fmt.Errorf("nothing to update: pass --display-name, --description, --policy-type, --steps-file, --rules-file, or --body-file with --update-mask")
	}

	return policy, strings.Join(paths, ","), nil
}

func init() {
	policiesUpdateCmd.Flags().String("display-name", "", "New display name")
	policiesUpdateCmd.Flags().String("description", "", "New description")
	policiesUpdateCmd.Flags().String("policy-type", "", "New policy type: grant, revoke, certify (rarely changed)")
	policiesUpdateCmd.Flags().String("steps-file", "", "JSON array: replaces the baseline policySteps.steps content (file, or \"-\" for stdin)")
	policiesUpdateCmd.Flags().String("rules-file", "", "JSON array: replaces rules[] (file, or \"-\" for stdin)")
	policiesUpdateCmd.Flags().String("body-file", "", "Full Policy JSON object, verbatim (file, or \"-\" for stdin); requires --update-mask; mutually exclusive with the flags above")
	policiesUpdateCmd.Flags().String("update-mask", "", "Comma-separated field paths to update (required with --body-file; ignored/derived otherwise)")
	policiesUpdateCmd.Flags().Bool("allow-deny-all", false, "Allow leaving/setting a policy with no steps — bypasses the empty-steps guard on purpose")
	policiesCmd.AddCommand(policiesUpdateCmd)
}
