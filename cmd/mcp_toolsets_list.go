package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpToolsetsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List MCP toolsets for a connector (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id", "connector-id"); err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newListClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		appID, _ := cmd.Flags().GetString("app-id")
		connectorID, _ := cmd.Flags().GetString("connector-id")
		requestedPageSize := pageSizeFlag(cmd)
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		enc := newEmitter(cmd)
		guard := newPaginationCursorGuard()
		route := "GET " + client.Path("/api/v1/apps/%s/connectors/%s/mcp_toolsets", appID, connectorID)
		for !limitReached(enc.Written(), limit) {
			if guard.tokenAlreadyUsed(pageToken) {
				return guard.warnStuck(cmd, route)
			}
			pageSize := requestedPageSize
			if !enc.Filtered() {
				pageSize = effectivePageSize(requestedPageSize, limit, enc.Written())
			}
			params := map[string]string{
				"page_size": strconv.Itoa(pageSize),
			}
			if pageToken != "" {
				params["page_token"] = pageToken
			}

			path := client.Path("/api/v1/apps/%s/connectors/%s/mcp_toolsets", appID, connectorID)
			data, err := c.Get(cmd.Context(), path, params)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				Profiles      []toolsetView `json:"profiles"`
				NextPageToken string        `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			ids := make([]string, len(resp.Profiles))
			for i, it := range resp.Profiles {
				ids[i] = it.ID
			}
			keep, err := guard.checkPage(cmd, route, ids)
			if err != nil {
				return err
			}
			resp.Profiles = keepRows(resp.Profiles, keep)
			for _, p := range resp.Profiles {
				if err := enc.Encode(toolsetRow(p)); err != nil {
					return err
				}
				if limitReached(enc.Written(), limit) {
					return nil
				}
			}

			if resp.NextPageToken == "" || manualPaging {
				break
			}
			guard.markTokenUsed(pageToken)
			pageToken = resp.NextPageToken
		}

		return nil
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
		"id":                 t.ID,
		"app_id":             t.AppID,
		"connector_id":       t.ConnectorID,
		"display_name":       t.DisplayName,
		"description":        t.Description,
		"app_entitlement_id": t.AppEntitlementID,
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
