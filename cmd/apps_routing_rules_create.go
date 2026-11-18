package cmd

import (
	"net/http"

	"github.com/spf13/cobra"
)

var appsRoutingRulesCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an entitlement routing rule on an app (pretty JSON)",
	Long: `Create an entitlement routing rule on an app via POST
/api/v1/apps/{app-id}/entitlement_configuration_rules.

The rule is created disabled unless --enabled is given; an enabled rule routes
real access requests. --condition is a CEL expression; omitted, the rule
matches every target no earlier rule matched. An invalid expression is a 400
(exit 2) quoting the parse error.

--priority is a 1-indexed insertion slot: the rule goes there and the rules
after it move down one. A slot past the end, or omitting the flag, appends the
rule last. The server accepts 0 to 1000; anything else is a 400 (exit 2).

The settings flags set what a matching request gets. The server checks the
policy and schema ids: an unknown one is a 404 (exit 4, e.g. "request policy
not found"), and a policy that is not a grant policy is a 400 (exit 2,
"request_policy_id must reference a grant policy"). --emergency-grant-enabled
needs --emergency-grant-policy-id ("emergency_grant_policy_id is required when
emergency_grant_enabled is true").

An app holds a limited number of rules (10 when measured). Past the limit the
server answers 400 (exit 2):
  This app already has the maximum of 10 entitlement configuration rules. Delete a rule to add another.

An unknown app exits 4.

Prints the created rule, unwrapped like "apps routing-rules get": the id is
.id. Honors --dry-run.

Example:
  c1i apps routing-rules create --app-id <app-id> --display-name "Engineering" \
    --condition 'subject.department == "Engineering"' --request-policy-id <policy-id>`,
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		body := map[string]any{"displayName": mustGetString(cmd, "display-name")}
		body["enabled"], _ = cmd.Flags().GetBool("enabled")
		for flag, key := range map[string]string{"description": "description", "condition": "condition"} {
			if flagGiven(cmd, flag) {
				body[key] = mustGetString(cmd, flag)
			}
		}
		if flagGiven(cmd, "priority") {
			body["priority"], _ = cmd.Flags().GetInt("priority")
		}
		settings, err := routingRuleSettings(cmd)
		if err != nil {
			return err
		}
		if len(settings) > 0 {
			body["settings"] = settings
		}
		return sendMutation(cmd, mutation{
			Method: http.MethodPost,
			Path:   routingRulesPath(appID),
			Body:   body,
			Unwrap: named("routingRule"),
		})
	},
}

// routingRuleSettings builds the rule's settings object from the settings
// flags that were given; empty when none was.
func routingRuleSettings(cmd *cobra.Command) (map[string]any, error) {
	settings := map[string]any{}
	for flag, key := range map[string]string{
		"request-policy-id":         "requestPolicyId",
		"emergency-grant-policy-id": "emergencyGrantPolicyId",
		"request-schema-id":         "requestSchemaId",
	} {
		if flagGiven(cmd, flag) {
			settings[key] = mustGetString(cmd, flag)
		}
	}
	if flagGiven(cmd, "emergency-grant-enabled") {
		settings["emergencyGrantEnabled"], _ = cmd.Flags().GetBool("emergency-grant-enabled")
	}
	d, ok, err := protoDurationFlag(cmd, "max-grant-duration")
	if err != nil {
		return nil, err
	}
	if ok {
		settings["maxGrantDuration"] = d
	}
	return settings, nil
}

// addRoutingRuleSettingsFlags registers the settings flags routingRuleSettings reads.
func addRoutingRuleSettingsFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("request-policy-id", "", "Grant policy a matching request uses")
	f.String("emergency-grant-policy-id", "", "Grant policy for emergency access requests")
	f.Bool("emergency-grant-enabled", false, "Allow emergency access requests (needs --emergency-grant-policy-id)")
	addProtoDurationFlag(cmd, "max-grant-duration", "Longest grant a matching request may ask for, as a protobuf duration, e.g. 3600s, or a Go duration such as 8h")
	f.String("request-schema-id", "", "Request schema (form) a matching request fills in")
}

func init() {
	f := appsRoutingRulesCreateCmd.Flags()
	f.String("app-id", "", "ID of the app to create the routing rule on")
	f.String("display-name", "", "Display name of the new rule")
	f.String("description", "", "Description of the new rule")
	f.String("condition", "", "CEL condition a target must match (omit to match every target no earlier rule matched)")
	f.Bool("enabled", false, "Create the rule enabled, so it routes real requests (default disabled)")
	f.Int("priority", 0, "1-indexed insertion slot (omit to append last)")
	addRoutingRuleSettingsFlags(appsRoutingRulesCreateCmd)
	markRequired(appsRoutingRulesCreateCmd, "app-id", "display-name")
	markMutating(appsRoutingRulesCreateCmd)
	appsRoutingRulesCmd.AddCommand(appsRoutingRulesCreateCmd)
}
