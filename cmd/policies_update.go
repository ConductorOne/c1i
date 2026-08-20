package cmd

import (
	"encoding/json"
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
any request is sent) — including the C57 empty-steps guard, which also
applies to update: clearing a policy's baseline steps entry (or replacing
it with an empty array) hits the exact same silent-deny-all default /
HTTP-500 crash as create does. --allow-deny-all bypasses the deny-all case
(not the crash case, which is never safe to bypass).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		allowDenyAll, _ := cmd.Flags().GetBool("allow-deny-all")

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		// Lazily fetches the existing policy's type, only when --steps-file
		// is used without an explicit --policy-type on this update (needed
		// to resolve the policySteps baseline map key). Memoized so a
		// plain display-name/description update never pays for it, and a
		// steps-file update only pays for it once.
		var c *client.Client
		var fetchedType string
		var fetchedOnce bool
		resolveFallbackPolicyType := func() (string, error) {
			if fetchedOnce {
				return fetchedType, nil
			}
			fetchedOnce = true
			c, err = newPoliciesClient(cmd, baseURL)
			if err != nil {
				return "", fmt.Errorf("authentication failed: %w", err)
			}
			fetchedType, err = fetchPolicyType(cmd, c, id)
			if err != nil {
				return "", err
			}
			return fetchedType, nil
		}

		policy, mask, err := buildUpdatePolicyPatch(cmd, resolveFallbackPolicyType)
		if err != nil {
			return &usageError{err}
		}

		effectivePolicyType, _ := policy["policyType"].(string)
		if effectivePolicyType == "" {
			effectivePolicyType = fetchedType
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

// fetchPolicyType looks up an existing policy's policyType, used only to
// resolve the policySteps baseline key (policyStepsKey) when --steps-file is
// supplied without an explicit --policy-type on update.
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
// I/O) when --steps-file is supplied without --policy-type. The "id" field
// is intentionally NOT set here — the caller adds it after guard
// validation, matching create's shape where the guards see exactly the
// fields being changed.
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
	policiesUpdateCmd.Flags().Bool("allow-deny-all", false, "Allow leaving/setting a policy with no steps — bypasses the C57 empty-steps guard on purpose")
	policiesCmd.AddCommand(policiesUpdateCmd)
}
