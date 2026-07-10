package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpToolsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List MCP tools discovered for a connector (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id", "connector-id"); err != nil {
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
		requestedPageSize := clampPageSize(getIntFlag(cmd, "page-size"))
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		enc := newEmitter(cmd.OutOrStdout())
		emitted := 0
		for !limitReached(emitted, limit) {
			pageSize := effectivePageSize(requestedPageSize, limit, emitted)
			params := map[string]string{
				"page_size": strconv.Itoa(pageSize),
			}
			if pageToken != "" {
				params["page_token"] = pageToken
			}

			path := client.Path("/api/v1/apps/%s/connectors/%s/mcp_tools", appID, connectorID)
			data, err := c.Get(cmd.Context(), path, params)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				Tools         []toolView `json:"tools"`
				NextPageToken string     `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, t := range resp.Tools {
				_ = enc.Encode(toolRow(t))
				emitted++
				if limitReached(emitted, limit) {
					return nil
				}
			}

			if resp.NextPageToken == "" || manualPaging {
				break
			}
			pageToken = resp.NextPageToken
		}

		return nil
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
}

func toolRow(t toolView) map[string]string {
	return map[string]string{
		"id":                 t.ID,
		"app_id":             t.AppID,
		"connector_id":       t.ConnectorID,
		"tool_name":          t.ToolName,
		"app_entitlement_id": t.AppEntitlementID,
		"display_name":       firstNonEmpty(t.DisplayName, t.DefaultDisplayName),
		"description":        t.Description,
		"classification":     t.Classification,
		"state":              t.State,
		"visibility":         firstNonEmpty(t.Visibility, t.DefaultVisibility),
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
	mcpToolsListCmd.Flags().Int("page-size", 50, "Results per page (max 100)")
	mcpToolsListCmd.Flags().String("page-token", "", "Pagination cursor")
	markRequired(mcpToolsListCmd, "app-id", "connector-id")
	addLimitFlag(mcpToolsListCmd)
	mcpToolsCmd.AddCommand(mcpToolsListCmd)
}
