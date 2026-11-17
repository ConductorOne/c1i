package cmd

import (
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

		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		appID, _ := cmd.Flags().GetString("app-id")
		query, _ := cmd.Flags().GetString("query")
		toolState, err := toolStateFlag.as("tool-state").parse(mustGetString(cmd, "tool-state"))
		if err != nil {
			return err
		}
		includeLastCalled, _ := cmd.Flags().GetBool("include-last-called-at")
		body := map[string]any{"appId": appID}
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
		return listPages(cmd, c, pageSpec[serverSearchHit]{
			Route: "POST " + path,
			Fetch: postPage(cmd.Context(), path, body),
			Parse: parseList[serverSearchHit],
			ID:    func(it serverSearchHit) string { return it.MCPServer.ConnectorID },
			Rows: rowFn(func(r serverSearchHit) map[string]any {
				var toolCount *int64
				if toolState != "" {
					tc := int64(r.ToolCount)
					toolCount = &tc
				}
				return serverCountRow(r.MCPServer, toolCount)
			}),
		})
	},
}

// serverSearchHit is one row of the mcp_servers/search response.
type serverSearchHit struct {
	MCPServer serverView `json:"mcpServer"`
	ToolCount flexInt64  `json:"toolCount"`
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
