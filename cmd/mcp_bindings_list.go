package cmd

import (
	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpBindingsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tool bindings for an MCP toolset (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {

		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		appID, _ := cmd.Flags().GetString("app-id")
		connectorID, _ := cmd.Flags().GetString("connector-id")
		toolsetID, _ := cmd.Flags().GetString("toolset-id")
		path := client.Path("/api/v1/apps/%s/connectors/%s/mcp_toolsets/%s/tool_bindings", appID, connectorID, toolsetID)
		// Rows carry no id of their own, so only the token and idle-page checks apply.
		return listPages(cmd, c, pageSpec[toolBinding]{
			Route: "GET " + path,
			Fetch: getPage(cmd.Context(), path, nil),
			Parse: parseKeyed[toolBinding]("bindings"),
			Rows: rowFn(func(b toolBinding) map[string]any {
				return map[string]any{
					"app_id":            nilIfEmpty(b.AppID),
					"connector_id":      nilIfEmpty(b.ConnectorID),
					"access_profile_id": nilIfEmpty(b.AccessProfileID),
					"mcp_tool_id":       nilIfEmpty(b.MCPToolID),
					"created_at":        nilIfEmpty(b.CreatedAt),
				}
			}),
		})
	},
}

// toolBinding is one row of the tool_bindings response.
type toolBinding struct {
	AppID           string `json:"appId"`
	ConnectorID     string `json:"connectorId"`
	AccessProfileID string `json:"accessProfileId"`
	MCPToolID       string `json:"mcpToolId"`
	CreatedAt       string `json:"createdAt"`
}

func init() {
	mcpBindingsListCmd.Flags().String("app-id", "", "Application ID")
	mcpBindingsListCmd.Flags().String("connector-id", "", "Connector ID")
	mcpBindingsListCmd.Flags().String("toolset-id", "", "MCP toolset (access profile) ID")
	addPaginationFlags(mcpBindingsListCmd)
	markRequired(mcpBindingsListCmd, "app-id", "connector-id", "toolset-id")
	mcpBindingsCmd.AddCommand(mcpBindingsListCmd)
}
