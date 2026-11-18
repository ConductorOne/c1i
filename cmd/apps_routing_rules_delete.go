package cmd

import (
	"net/http"

	"github.com/spf13/cobra"
)

var appsRoutingRulesDeleteCmd = &cobra.Command{
	Use:   "delete <rule-id>",
	Short: "Delete one of an app's entitlement routing rules",
	Long: `Delete an entitlement routing rule of an app. Honors --dry-run.

The delete is soft: "apps routing-rules get" still returns the rule with
deletedAt set, and "apps routing-rules list" stops listing it. The remaining
rules keep their order and are renumbered 1..N. Repeating the delete, or an
unknown app or rule, exits 4; a soft-deleted app is reported as unknown (4),
as the API answers the delete.

Prints the API's response, which is {}; exit 0 is the success signal.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: routingRulePath(appID, args[0])})
	},
}

func init() {
	addRoutingRuleAppFlag(appsRoutingRulesDeleteCmd)
	markMutating(appsRoutingRulesDeleteCmd)
	appsRoutingRulesCmd.AddCommand(appsRoutingRulesDeleteCmd)
}
