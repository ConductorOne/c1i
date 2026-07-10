package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpToolsetsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an MCP toolset (pretty JSON)",
	Long: `Create an MCP toolset (access profile) under a connector.

A toolset is an admin-curated grouping of MCP tools bound to a single
AppEntitlement so users can request/get granted the whole bundle at once.
After creating, attach tools with "c1i mcp bindings create".`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id", "connector-id", "display-name"); err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		appID, _ := cmd.Flags().GetString("app-id")
		connectorID, _ := cmd.Flags().GetString("connector-id")
		displayName, _ := cmd.Flags().GetString("display-name")
		description, _ := cmd.Flags().GetString("description")

		body := map[string]any{
			"appId":       appID,
			"connectorId": connectorID,
			"displayName": displayName,
		}
		if description != "" {
			body["description"] = description
		}

		path := client.Path("/api/v1/apps/%s/connectors/%s/mcp_toolsets", appID, connectorID)
		if dryRunActive() {
			return printDryRun(cmd, "POST", path, body)
		}
		data, err := c.Post(cmd.Context(), path, body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeRawObject(cmd, data)
	},
}

func init() {
	mcpToolsetsCreateCmd.Flags().String("app-id", "", "Application ID")
	mcpToolsetsCreateCmd.Flags().String("connector-id", "", "Connector ID")
	mcpToolsetsCreateCmd.Flags().String("display-name", "", "Toolset display name")
	mcpToolsetsCreateCmd.Flags().String("description", "", "Toolset description")
	markRequired(mcpToolsetsCreateCmd, "app-id", "connector-id", "display-name")
	mcpToolsetsCmd.AddCommand(mcpToolsetsCreateCmd)
}
