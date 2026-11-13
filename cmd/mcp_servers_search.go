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
		toolState, err := toolStateFlag.as("tool-state").parse(mustGetString(cmd, "tool-state"))
		if err != nil {
			return err
		}
		includeLastCalled, _ := cmd.Flags().GetBool("include-last-called-at")
		requestedPageSize := pageSizeFlag(cmd)
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		enc := newEmitter(cmd)
		guard := newPaginationCursorGuard()
		route := "POST " + client.Path("/api/v1/apps/%s/mcp_servers/search", appID)
		for !limitReached(enc.Written(), limit) {
			if guard.tokenAlreadyUsed(pageToken) {
				return guard.warnStuck(cmd, route)
			}
			pageSize := requestedPageSize
			if !enc.Filtered() {
				pageSize = effectivePageSize(requestedPageSize, limit, enc.Written())
			}
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
				body["toolState"] = toolState
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

			ids := make([]string, len(resp.List))
			for i, it := range resp.List {
				ids[i] = it.MCPServer.ConnectorID
			}
			keep, err := guard.checkPage(cmd, route, ids)
			if err != nil {
				return err
			}
			resp.List = keepRows(resp.List, keep)
			for _, r := range resp.List {
				var toolCount *int64
				if toolState != "" {
					tc := int64(r.ToolCount)
					toolCount = &tc
				}
				if err := enc.Encode(serverCountRow(r.MCPServer, toolCount)); err != nil {
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

func init() {
	mcpServersSearchCmd.Flags().String("app-id", "", "Application ID")
	mcpServersSearchCmd.Flags().String("query", "", "Fuzzy search on display_name")
	mcpServersSearchCmd.Flags().String("tool-state", "", "Tool state to count: "+toolStateFlag.usage()+". Without it, tool_count is omitted (the server does not compute a count)")
	// Picks which tools to count; it does not narrow the rows.
	allowEmpty(mcpServersSearchCmd, emptyIsOmitted, "tool-state")
	mcpServersSearchCmd.Flags().Bool("include-last-called-at", false, "Populate last_called_at per row (extra read per server)")
	addPaginationFlags(mcpServersSearchCmd)
	markRequired(mcpServersSearchCmd, "app-id")
	mcpServersCmd.AddCommand(mcpServersSearchCmd)
}
