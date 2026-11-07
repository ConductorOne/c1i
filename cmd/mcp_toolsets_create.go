package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpToolsetsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an MCP toolset (pretty JSON)",
	Long: `Create an MCP toolset (access profile) under a connector.

A toolset is an admin-curated grouping of MCP tools bound to a single
AppEntitlement so users can request/get granted the whole bundle at once.
After creating, attach tools with "c1i mcp bindings create".

Prints the toolset, unwrapped like "mcp toolsets get": the id is .id. Extract
it with jq -er, which fails on a --dry-run preview where jq -r exits 0 with
null.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := requireNonEmptyIfSet(cmd, "display-name"); err != nil {
			return err
		}
		if err := requireNonEmpty(cmd, "app-id", "connector-id", "display-name"); err != nil {
			return err
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
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: body})
	},
}

func init() {
	mcpToolsetsCreateCmd.Flags().String("app-id", "", "Application ID")
	mcpToolsetsCreateCmd.Flags().String("connector-id", "", "Connector ID")
	mcpToolsetsCreateCmd.Flags().String("display-name", "", "Toolset display name ("+displayNameRule+")")
	mcpToolsetsCreateCmd.Flags().String("description", "", "Toolset description")
	markRequired(mcpToolsetsCreateCmd, "app-id", "connector-id", "display-name")
	markMutating(mcpToolsetsCreateCmd)
	mcpToolsetsCmd.AddCommand(mcpToolsetsCreateCmd)
}
