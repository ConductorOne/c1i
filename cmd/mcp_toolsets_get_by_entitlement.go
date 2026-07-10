package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpToolsetsGetByEntitlementCmd = &cobra.Command{
	Use:   "get-by-entitlement",
	Short: "Resolve a toolset by its AppEntitlement ID (pretty JSON)",
	Long: `Look up the toolset that owns a given AppEntitlement.

Each toolset creates one AppEntitlement at sync time; this is the reverse
lookup — given an entitlement ID (e.g. from an access-request task), find
which toolset it represents.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id", "app-entitlement-id"); err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := client.New(cmd.Context(), baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		appID, _ := cmd.Flags().GetString("app-id")
		entID, _ := cmd.Flags().GetString("app-entitlement-id")

		path := client.Path("/api/v1/apps/%s/mcp_toolsets/by_app_entitlement_id/%s", appID, entID)
		data, err := c.Get(cmd.Context(), path, nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		var pretty bytes.Buffer
		if err := json.Indent(&pretty, data, "", "  "); err != nil {
			return fmt.Errorf("failed to pretty-print response: %w", err)
		}
		_, _ = pretty.WriteTo(cmd.OutOrStdout())
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		return nil
	},
}

func init() {
	mcpToolsetsGetByEntitlementCmd.Flags().String("app-id", "", "Application ID")
	mcpToolsetsGetByEntitlementCmd.Flags().String("app-entitlement-id", "", "AppEntitlement ID to look up")
	markRequired(mcpToolsetsGetByEntitlementCmd, "app-id", "app-entitlement-id")
	mcpToolsetsCmd.AddCommand(mcpToolsetsGetByEntitlementCmd)
}
