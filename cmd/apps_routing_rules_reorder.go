package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var appsRoutingRulesReorderCmd = &cobra.Command{
	Use:   "reorder",
	Short: "Set the evaluation order of an app's entitlement routing rules (pretty JSON)",
	Long: `Set the evaluation order of an app's entitlement routing rules. Pass every
active rule's id once with --rule-id, in the order wanted: the first gets
priority 1, the next 2, and so on. Get the ids from "apps routing-rules list".

The server refuses (400, exit 2) a list that is not exactly the app's active
rules, quoting the problem, e.g.:
  rule_ids must be a permutation of active rules for the app: got 3 ids for 4 active rules
A missing, extra, repeated, unknown or deleted id is refused this way; nothing
is reordered. An unknown app exits 4.

Prints the API's response, {"list": [...]}: every rule in its new order, each
shaped like "apps routing-rules get". Honors --dry-run.

Example:
  c1i apps routing-rules reorder --app-id <app-id> --rule-id <first-id> --rule-id <second-id>`,
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		ids, err := repeatableStringFlag(cmd, "rule-id")
		if err != nil {
			return err
		}
		return sendMutation(cmd, mutation{
			Method: http.MethodPost,
			Path:   client.Path("/api/v1/apps/%s/entitlement_configuration_rules/reorder", appID),
			Body:   map[string]any{"ruleIds": ids},
		})
	},
}

func init() {
	addRoutingRuleAppFlag(appsRoutingRulesReorderCmd)
	addRepeatableStringFlag(appsRoutingRulesReorderCmd, "rule-id", "Rule id, in evaluation order (repeatable; every active rule exactly once)")
	markRequired(appsRoutingRulesReorderCmd, "rule-id")
	markMutating(appsRoutingRulesReorderCmd)
	appsRoutingRulesCmd.AddCommand(appsRoutingRulesReorderCmd)
}
