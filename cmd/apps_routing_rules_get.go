package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var appsRoutingRulesGetCmd = &cobra.Command{
	Use:   "get <rule-id>",
	Short: "Get one of an app's entitlement routing rules (pretty JSON)",
	Long: `Get one entitlement routing rule of an app (pretty JSON), unwrapped: the
rule's own keys are at the top level and its id is .id. A deleted rule is
still returned, with deletedAt set.

An unknown app or rule exits 4.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}
		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		appID, _ := cmd.Flags().GetString("app-id")
		data, err := c.Get(cmd.Context(), routingRulePath(appID, args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeNamedResource(cmd, data, "routingRule")
	},
}

func init() {
	addRoutingRuleAppFlag(appsRoutingRulesGetCmd)
	appsRoutingRulesCmd.AddCommand(appsRoutingRulesGetCmd)
}
