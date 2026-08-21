package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpServersSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search MCP servers within an app, with per-server tool counts (NDJSON output)",
	Long: `Search the MCP servers registered under a single app (app_id required),
returning each server plus, when --tool-state is given, a count of its tools
in that state.

The server only computes a count when --tool-state is set; a filterless
search leaves the count uncomputed rather than "all tools," so "tool_count"
is omitted from the row instead of showing a 0 that would look identical to
a server with no tools (verified live: a server with hundreds of tools in
other states still returns 0 for a filterless search).
--include-last-called-at adds a "last_called_at" timestamp per row (one extra
TSDB read each) so you can spot idle servers.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id"); err != nil {
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
		query, _ := cmd.Flags().GetString("query")
		toolState, _ := cmd.Flags().GetString("tool-state")
		includeLastCalled, _ := cmd.Flags().GetBool("include-last-called-at")
		requestedPageSize := clampPageSize(getIntFlag(cmd, "page-size"))
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		enc := newEmitter(cmd)
		emitted := 0
		for !limitReached(emitted, limit) {
			pageSize := effectivePageSize(requestedPageSize, limit, emitted)
			body := map[string]any{
				"appId":    appID,
				"pageSize": pageSize,
			}
			if pageToken != "" {
				body["pageToken"] = pageToken
			}
			if query != "" {
				body["query"] = query
			}
			if toolState != "" {
				body["toolState"] = mapToolState(toolState)
			}
			if includeLastCalled {
				body["includeLastCalledAt"] = true
			}

			path := client.Path("/api/v1/apps/%s/mcp_servers/search", appID)
			data, err := c.Post(cmd.Context(), path, body)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List []struct {
					MCPServer serverView `json:"mcpServer"`
					ToolCount flexInt64  `json:"toolCount"`
				} `json:"list"`
				NextPageToken string `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, r := range resp.List {
				var toolCount *int64
				if toolState != "" {
					tc := int64(r.ToolCount)
					toolCount = &tc
				}
				_ = enc.Encode(serverCountRow(r.MCPServer, toolCount))
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

func init() {
	mcpServersSearchCmd.Flags().String("app-id", "", "Application ID")
	mcpServersSearchCmd.Flags().String("query", "", "Fuzzy search on display_name")
	mcpServersSearchCmd.Flags().String("tool-state", "", "Tool state to count: pending, approved, disabled, removed. Without it, tool_count is omitted (the server does not compute a count)")
	mcpServersSearchCmd.Flags().Bool("include-last-called-at", false, "Populate last_called_at per row (extra read per server)")
	mcpServersSearchCmd.Flags().Int("page-size", 50, "Results per page (max 100)")
	mcpServersSearchCmd.Flags().String("page-token", "", "Pagination cursor")
	markRequired(mcpServersSearchCmd, "app-id")
	addLimitFlag(mcpServersSearchCmd)
	mcpServersCmd.AddCommand(mcpServersSearchCmd)
}
