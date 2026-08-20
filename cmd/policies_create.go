package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var policiesCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new policy (pretty JSON)",
	Long: `Create a new policy.

The nested policySteps/rules structure is taken from JSON files (or "-" for
stdin), not modeled as flags — a policy's approval graph is deeply nested
(steps are a oneof of approval/provision/accept/reject/wait/form; an
approval step's approver is itself a oneof of ten arms) and would be
unusable as a flat flag surface. --display-name/--description/--policy-type
stay as flags for the simple top-level fields, mirroring how
"mcp servers register" takes its auth config via --hosted-config-file.

  --steps-file  a JSON ARRAY: the "steps" content for --policy-type's
                baseline entry, e.g.:
                  [{"approval":{"users":{"userIds":["u1"]}}}]
                This CLI wraps it under the correct policySteps map key for
                you (see the policyStepsKey doc comment for why the key
                matters: it is the lowercase type word "grant"/"revoke"/
                "certify", NOT the enum name).
  --rules-file  a JSON ARRAY of {"condition": "<CEL>", "stepKey": "<key>"}
                (or the deprecated {"condition":"...", "policyKey":"..."})
                routing rules, evaluated top to bottom; first match wins.
                A baseline/catch-all rule needs the literal condition
                "true" — an empty condition 400s.
  --body-file   the FULL CreatePolicyRequest body verbatim, for anything
                the flags above can't express (postActions, multiple
                policySteps entries, ...). Mutually exclusive with every
                other flag in this command.

Client-side guards (before any request is sent, exit code 2):
  - refuses to send empty/missing steps for the baseline entry — the server
    silently turns that into a deny-everything policy (a single
    {"reject":{}} step), with no validation error. Pass
    --allow-deny-all if that's actually what you want.
  - refuses policyType left unspecified, an empty rules[].condition, a rule
    that doesn't set exactly one outcome (stepKey/policyId vs. the
    deprecated policyKey), a rules[].stepKey with no matching (non-empty)
    policySteps entry, a "provision" step, fallback/fallbackUserIds on an
    approver arm that doesn't support them, and fallback:true with nothing
    to fall back to — several of these are bare server errors that surface
    as an opaque HTTP 500 rather than a 400, so catching them early is the
    only way to get a useful message.

CEL conditions run with root variable "subject" (not "user") — validate one
with "c1i policies validate-cel" before using it here (note: that command
validates the rules[].condition environment, not the different environment
ExpressionApproval.expressions run in — see its --help).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := buildCreatePolicyBody(cmd)
		if err != nil {
			return &usageError{err}
		}

		allowDenyAll, _ := cmd.Flags().GetBool("allow-deny-all")
		if err := validateCreateBody(body, allowDenyAll); err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		if dryRunActive() {
			return printDryRun(cmd, "POST", "/api/v1/policies", body)
		}

		c, err := newPoliciesClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Post(cmd.Context(), "/api/v1/policies", body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeRawObject(cmd, data)
	},
}

// buildCreatePolicyBody assembles the CreatePolicyRequest body from flags, or
// reads it verbatim from --body-file. Pure (no network/auth) so --dry-run and
// unit tests exercise the same body-building path the live request uses.
func buildCreatePolicyBody(cmd *cobra.Command) (map[string]any, error) {
	bodyFile, _ := cmd.Flags().GetString("body-file")
	usingFlags := cmd.Flags().Changed("display-name") || cmd.Flags().Changed("description") ||
		cmd.Flags().Changed("policy-type") || cmd.Flags().Changed("steps-file") || cmd.Flags().Changed("rules-file")

	if bodyFile != "" {
		if usingFlags {
			return nil, fmt.Errorf("--body-file is mutually exclusive with --display-name/--description/--policy-type/--steps-file/--rules-file")
		}
		return readConfigFile(cmd, bodyFile)
	}

	displayName, _ := cmd.Flags().GetString("display-name")
	if displayName == "" {
		return nil, fmt.Errorf("--display-name is required (or pass --body-file)")
	}
	policyType := mapPolicyType(mustGetString(cmd, "policy-type"))

	body := map[string]any{
		"displayName": displayName,
		"policyType":  policyType,
	}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}

	if stepsFile, _ := cmd.Flags().GetString("steps-file"); stepsFile != "" {
		steps, err := readJSONArrayFile(cmd, stepsFile)
		if err != nil {
			return nil, fmt.Errorf("reading --steps-file: %w", err)
		}
		key, err := policyStepsKey(policyType)
		if err != nil {
			return nil, err
		}
		body["policySteps"] = map[string]any{key: map[string]any{"steps": steps}}
	}

	if rulesFile, _ := cmd.Flags().GetString("rules-file"); rulesFile != "" {
		rules, err := readJSONArrayFile(cmd, rulesFile)
		if err != nil {
			return nil, fmt.Errorf("reading --rules-file: %w", err)
		}
		body["rules"] = rules
	}

	return body, nil
}

// mustGetString returns a string flag's value, ignoring the lookup error
// (cobra only errors when the flag isn't registered, a programming bug).
func mustGetString(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}

func init() {
	policiesCreateCmd.Flags().String("display-name", "", "Display name for the new policy")
	policiesCreateCmd.Flags().String("description", "", "Description for the new policy")
	policiesCreateCmd.Flags().String("policy-type", "", "Policy type: grant, revoke, certify (access_request is deprecated; provision is server-internal)")
	policiesCreateCmd.Flags().String("steps-file", "", "JSON array: the baseline policySteps.steps content (file, or \"-\" for stdin)")
	policiesCreateCmd.Flags().String("rules-file", "", "JSON array of routing rules: [{\"condition\":\"<CEL>\",\"stepKey\":\"<key>\"}] (file, or \"-\" for stdin)")
	policiesCreateCmd.Flags().String("body-file", "", "Full CreatePolicyRequest JSON body, verbatim (file, or \"-\" for stdin); mutually exclusive with the flags above")
	policiesCreateCmd.Flags().Bool("allow-deny-all", false, "Allow creating a policy with no steps (or an explicit deny-all) — bypasses the C57 empty-steps guard on purpose")
	annotateRequired(policiesCreateCmd, "display-name", "policy-type")
	policiesCmd.AddCommand(policiesCreateCmd)
}
