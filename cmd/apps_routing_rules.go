package cmd

import (
	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var appsRoutingRulesCmd = &cobra.Command{
	Use:   "routing-rules",
	Short: "Manage an app's entitlement routing rules",
	Long: `Manage an app's entitlement routing rules (the API's entitlement
configuration rules): list, get, create, update, delete and reorder. Rules are
evaluated in priority order; the first enabled rule whose CEL condition
matches supplies the entitlement's request settings (request policy, emergency
grant policy, maximum grant duration, request schema). Each rule belongs to
the app given by --app-id.

An enabled rule routes real access requests. Create a rule disabled (the
default) to stage it.`,
}

// routingRulesPath is the collection of one app's routing rules.
func routingRulesPath(appID string) string {
	return client.Path("/api/v1/apps/%s/entitlement_configuration_rules", appID)
}

// routingRulePath addresses one routing rule of an app.
func routingRulePath(appID, id string) string {
	return client.Path("/api/v1/apps/%s/entitlement_configuration_rules/%s", appID, id)
}

// addRoutingRuleAppFlag registers the parent --app-id every routing-rules
// subcommand takes.
func addRoutingRuleAppFlag(cmd *cobra.Command) {
	cmd.Flags().String("app-id", "", "ID of the app the routing rule belongs to")
	markRequired(cmd, "app-id")
}

// routingRuleRow is the NDJSON row of "apps routing-rules list". The
// settings are flattened; an empty condition (null here) matches every
// target no earlier rule matched.
func routingRuleRow(item map[string]any) map[string]any {
	s, _ := item["settings"].(map[string]any)
	return map[string]any{
		"id":                        optString(item, "id"),
		"app_id":                    optString(item, "appId"),
		"display_name":              optString(item, "displayName"),
		"description":               optString(item, "description"),
		"priority":                  item["priority"],
		"enabled":                   boolField(item, "enabled"),
		"condition":                 optString(item, "condition"),
		"request_policy_id":         optString(s, "requestPolicyId"),
		"emergency_grant_policy_id": optString(s, "emergencyGrantPolicyId"),
		"emergency_grant_enabled":   boolField(s, "emergencyGrantEnabled"),
		"max_grant_duration":        optString(s, "maxGrantDuration"),
		"request_schema_id":         optString(s, "requestSchemaId"),
		"created_at":                optString(item, "createdAt"),
		"updated_at":                optString(item, "updatedAt"),
	}
}

var appsRoutingRulesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List an app's entitlement routing rules (NDJSON output)",
	Long: `List an app's entitlement routing rules in evaluation order (NDJSON
output). Rows carry id, app_id, display_name, description, priority (1 is
evaluated first), enabled, condition (null: matches every target no earlier
rule matched), the settings flattened (request_policy_id,
emergency_grant_policy_id, emergency_grant_enabled, max_grant_duration,
request_schema_id), created_at and updated_at. A deleted rule is not listed.

The endpoint takes no page_size or page_token and returns the whole set.
A well-formed but unknown app returns zero rows and exit 0.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		return getFixedList(cmd, routingRulesPath(appID), routingRuleRow)
	},
}

func init() {
	addRoutingRuleAppFlag(appsRoutingRulesListCmd)
	addLimitFlag(appsRoutingRulesListCmd)
	appsRoutingRulesCmd.AddCommand(appsRoutingRulesListCmd)
	appsCmd.AddCommand(appsRoutingRulesCmd)
}
