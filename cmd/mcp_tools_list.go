package cmd

import (
	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpToolsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List MCP tools discovered for a connector (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {

		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		appID, _ := cmd.Flags().GetString("app-id")
		connectorID, _ := cmd.Flags().GetString("connector-id")
		path := client.Path("/api/v1/apps/%s/connectors/%s/mcp_tools", appID, connectorID)
		return listPages(cmd, c, pageSpec[toolView]{
			Route: "GET " + path,
			Fetch: getPage(cmd.Context(), path, nil),
			Parse: parseKeyed[toolView]("tools"),
			ID:    func(it toolView) string { return it.ID },
			Rows:  rowFn(toolRow),
		})
	},
}

// toolView mirrors the subset of MCPTool fields the CLI emits. Keeping it
// in one place lets list/get/search share the same JSON row shape.
type toolView struct {
	ID                 string `json:"id"`
	AppID              string `json:"appId"`
	ConnectorID        string `json:"connectorId"`
	ToolName           string `json:"toolName"`
	AppEntitlementID   string `json:"appEntitlementId"`
	DisplayName        string `json:"displayName"`
	Description        string `json:"description"`
	DefaultDisplayName string `json:"defaultDisplayName"`
	Classification     string `json:"classification"`
	State              string `json:"state"`
	Visibility         string `json:"visibility"`
	DefaultVisibility  string `json:"defaultVisibility"`
	DeletedAt          string `json:"deletedAt"`
}

func toolRow(t toolView) map[string]any {
	return map[string]any{
		"id":                 nilIfEmpty(t.ID),
		"app_id":             nilIfEmpty(t.AppID),
		"connector_id":       nilIfEmpty(t.ConnectorID),
		"tool_name":          nilIfEmpty(t.ToolName),
		"app_entitlement_id": nilIfEmpty(t.AppEntitlementID),
		"display_name":       nilIfEmpty(firstNonEmpty(t.DisplayName, t.DefaultDisplayName)),
		"description":        nilIfEmpty(t.Description),
		"classification":     nilIfEmpty(t.Classification),
		"state":              nilIfEmpty(t.State),
		"visibility":         nilIfEmpty(firstNonEmpty(t.Visibility, t.DefaultVisibility)),
		"deleted_at":         nilIfEmpty(t.DeletedAt),
	}
}

// firstNonEmpty folds an admin override + discovery-time default into one
// column: prefer the admin value, fall back to the default. Without this the
// emitted display_name / visibility cells are blank for any tool the admin
// hasn't customized — even when the server has a default to show.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func init() {
	mcpToolsListCmd.Flags().String("app-id", "", "Application ID")
	mcpToolsListCmd.Flags().String("connector-id", "", "Connector ID")
	addPaginationFlags(mcpToolsListCmd)
	markRequired(mcpToolsListCmd, "app-id", "connector-id")
	mcpToolsCmd.AddCommand(mcpToolsListCmd)
}
