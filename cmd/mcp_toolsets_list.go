package cmd

import (
	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpToolsetsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List MCP toolsets for a connector (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {

		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		appID, _ := cmd.Flags().GetString("app-id")
		connectorID, _ := cmd.Flags().GetString("connector-id")
		path := client.Path("/api/v1/apps/%s/connectors/%s/mcp_toolsets", appID, connectorID)
		return listPages(cmd, c, pageSpec[toolsetView]{
			Route: "GET " + path,
			Fetch: getPage(cmd.Context(), path, nil),
			Parse: parseKeyed[toolsetView]("profiles"),
			ID:    func(it toolsetView) string { return it.ID },
			Rows:  rowFn(toolsetRow),
		})
	},
}

type toolsetView struct {
	ID               string `json:"id"`
	AppID            string `json:"appId"`
	ConnectorID      string `json:"connectorId"`
	DisplayName      string `json:"displayName"`
	Description      string `json:"description"`
	AppEntitlementID string `json:"appEntitlementId"`
	ToolCount        int32  `json:"toolCount"`
	DeletedAt        string `json:"deletedAt"`
}

func toolsetRow(t toolsetView) map[string]any {
	return map[string]any{
		"id":                 nilIfEmpty(t.ID),
		"app_id":             nilIfEmpty(t.AppID),
		"connector_id":       nilIfEmpty(t.ConnectorID),
		"display_name":       nilIfEmpty(t.DisplayName),
		"description":        nilIfEmpty(t.Description),
		"app_entitlement_id": nilIfEmpty(t.AppEntitlementID),
		"tool_count":         int64(t.ToolCount),
		"deleted_at":         nilIfEmpty(t.DeletedAt),
	}
}

func init() {
	mcpToolsetsListCmd.Flags().String("app-id", "", "Application ID")
	mcpToolsetsListCmd.Flags().String("connector-id", "", "Connector ID")
	addPaginationFlags(mcpToolsetsListCmd)
	markRequired(mcpToolsetsListCmd, "app-id", "connector-id")
	mcpToolsetsCmd.AddCommand(mcpToolsetsListCmd)
}
