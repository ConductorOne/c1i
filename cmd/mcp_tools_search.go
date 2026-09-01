package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpToolsSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search MCP tools with filters (NDJSON output)",
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
		query, _ := cmd.Flags().GetString("query")
		states, err := repeatableStringFlag(cmd, "state")
		if err != nil {
			return err
		}
		classes, err := repeatableStringFlag(cmd, "classification")
		if err != nil {
			return err
		}
		requestedPageSize := pageSizeFlag(cmd)
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		enc := newEmitter(cmd)
		for !limitReached(enc.Written(), limit) {
			pageSize := requestedPageSize
			if !enc.Filtered() {
				pageSize = effectivePageSize(requestedPageSize, limit, enc.Written())
			}
			body := map[string]any{
				"appId":       appID,
				"connectorId": connectorID,
				"pageSize":    pageSize,
			}
			if pageToken != "" {
				body["pageToken"] = pageToken
			}
			if query != "" {
				body["query"] = query
			}
			if len(states) > 0 {
				mapped := make([]string, 0, len(states))
				for _, s := range states {
					mapped = append(mapped, mapToolState(s))
				}
				body["stateFilter"] = mapped
			}
			if len(classes) > 0 {
				mapped := make([]string, 0, len(classes))
				for _, s := range classes {
					mapped = append(mapped, mapToolClassification(s))
				}
				body["classificationFilter"] = mapped
			}

			path := client.Path("/api/v1/apps/%s/connectors/%s/mcp_tools/search", appID, connectorID)
			data, err := c.Post(cmd.Context(), path, body)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			// SearchTools may key its results under "tools" (like ListTools)
			// or the generic "list"; accept either so a wrong guess doesn't
			// make search silently return nothing.
			var resp struct {
				Tools         []toolView `json:"tools"`
				List          []toolView `json:"list"`
				NextPageToken string     `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}
			items := resp.Tools
			if len(items) == 0 {
				items = resp.List
			}

			for _, t := range items {
				_ = enc.Encode(toolRow(t))
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
	mcpToolsSearchCmd.Flags().String("app-id", "", "Application ID")
	mcpToolsSearchCmd.Flags().String("connector-id", "", "Connector ID")
	mcpToolsSearchCmd.Flags().String("query", "", "Fuzzy search on tool_name or display_name")
	addRepeatableStringFlag(mcpToolsSearchCmd, "state", "Filter by state (repeatable): pending, approved, disabled, removed")
	addRepeatableStringFlag(mcpToolsSearchCmd, "classification", "Filter by classification (repeatable): read, write, destructive, sensitive, dangerous")
	addPaginationFlags(mcpToolsSearchCmd)
	markRequired(mcpToolsSearchCmd, "app-id", "connector-id")
	mcpToolsCmd.AddCommand(mcpToolsSearchCmd)
}
