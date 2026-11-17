package cmd

import (
	"github.com/spf13/cobra"
)

var mcpServersCatalogListCmd = &cobra.Command{
	Use:   "list",
	Short: "List MCP server catalog entries (NDJSON output)",
	Long: `List the HOSTED MCP server catalog entries. Filter by free-text --query
(matches display_name). Scope/maturity enum filters are not exposed over REST
yet (the SDK can't encode them) — page through and filter client-side if needed.

Rows include service_name, base_url, default_tool_prefix, and stable so
near-duplicate entries for the same service (e.g. "Slack" vs "Slack API" — one
is the vendor's hosted MCP endpoint, the other a thin wrapper over its plain
REST API) can be told apart without a round trip to "get". They also include
required_scope_count/optional_scope_count, a summary of that entry's OAuth
scope tiering (see "catalog get" for details and caveats).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		query, _ := cmd.Flags().GetString("query")
		extra := map[string]string{}
		if query != "" {
			extra["query"] = query
		}
		return listPages(cmd, c, pageSpec[catalogEntryView]{
			Route: "GET /api/v1/mcp_server_catalog",
			Fetch: getPage(cmd.Context(), "/api/v1/mcp_server_catalog", extra),
			Parse: parseList[catalogEntryView],
			ID:    func(it catalogEntryView) string { return it.ID },
			Rows:  rowFn(catalogEntryRow),
		})
	},
}

func init() {
	mcpServersCatalogListCmd.Flags().String("query", "", "Filter catalog entries by display name")
	addPaginationFlags(mcpServersCatalogListCmd)
	mcpServersCatalogCmd.AddCommand(mcpServersCatalogListCmd)
}
