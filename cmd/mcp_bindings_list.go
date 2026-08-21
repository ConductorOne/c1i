package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpBindingsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tool bindings for an MCP toolset (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id", "connector-id", "toolset-id"); err != nil {
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
		toolsetID, _ := cmd.Flags().GetString("toolset-id")
		requestedPageSize := clampPageSize(getIntFlag(cmd, "page-size"))
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		enc := newEmitter(cmd)
		for !limitReached(enc.Written(), limit) {
			pageSize := effectivePageSize(requestedPageSize, limit, enc.Written())
			params := map[string]string{
				"page_size": strconv.Itoa(pageSize),
			}
			if pageToken != "" {
				params["page_token"] = pageToken
			}

			path := client.Path("/api/v1/apps/%s/connectors/%s/mcp_toolsets/%s/tool_bindings", appID, connectorID, toolsetID)
			data, err := c.Get(cmd.Context(), path, params)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				Bindings []struct {
					AppID           string `json:"appId"`
					ConnectorID     string `json:"connectorId"`
					AccessProfileID string `json:"accessProfileId"`
					MCPToolID       string `json:"mcpToolId"`
					CreatedAt       string `json:"createdAt"`
				} `json:"bindings"`
				NextPageToken string `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, b := range resp.Bindings {
				_ = enc.Encode(map[string]string{
					"app_id":            b.AppID,
					"connector_id":      b.ConnectorID,
					"access_profile_id": b.AccessProfileID,
					"mcp_tool_id":       b.MCPToolID,
					"created_at":        b.CreatedAt,
				})
				if limitReached(enc.Written(), limit) {
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

func init() {
	mcpBindingsListCmd.Flags().String("app-id", "", "Application ID")
	mcpBindingsListCmd.Flags().String("connector-id", "", "Connector ID")
	mcpBindingsListCmd.Flags().String("toolset-id", "", "MCP toolset (access profile) ID")
	mcpBindingsListCmd.Flags().Int("page-size", 50, "Results per page (max 100)")
	mcpBindingsListCmd.Flags().String("page-token", "", "Pagination cursor")
	markRequired(mcpBindingsListCmd, "app-id", "connector-id", "toolset-id")
	addLimitFlag(mcpBindingsListCmd)
	mcpBindingsCmd.AddCommand(mcpBindingsListCmd)
}
