package cmd

import (
	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpToolsSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search MCP tools with filters (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {

		states, err := parseRepeatableEnum(cmd, toolStateFlag)
		if err != nil {
			return err
		}
		classes, err := parseRepeatableEnum(cmd, toolClassificationFlag)
		if err != nil {
			return err
		}
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		appID, _ := cmd.Flags().GetString("app-id")
		connectorID, _ := cmd.Flags().GetString("connector-id")
		query, _ := cmd.Flags().GetString("query")
		body := map[string]any{"appId": appID, "connectorId": connectorID}
		if query != "" {
			body["query"] = query
		}
		if len(states) > 0 {
			body["stateFilter"] = states
		}
		if len(classes) > 0 {
			body["classificationFilter"] = classes
		}
		path := client.Path("/api/v1/apps/%s/connectors/%s/mcp_tools/search", appID, connectorID)
		return listPages(cmd, c, pageSpec[toolView]{
			Route: "POST " + path,
			Fetch: postPage(cmd.Context(), path, body),
			// SearchTools may key its results under "tools" (like ListTools) or the
			// generic "list"; accept either so a wrong guess doesn't make search
			// silently return nothing.
			Parse: parseKeyed[toolView]("tools", "list"),
			ID:    func(it toolView) string { return it.ID },
			Rows:  rowFn(toolRow),
		})
	},
}

func init() {
	mcpToolsSearchCmd.Flags().String("app-id", "", "Application ID")
	mcpToolsSearchCmd.Flags().String("connector-id", "", "Connector ID")
	mcpToolsSearchCmd.Flags().String("query", "", "Fuzzy search on tool_name or display_name")
	addRepeatableStringFlag(mcpToolsSearchCmd, "state", "Filter by state (repeatable): "+toolStateFlag.usage())
	addRepeatableStringFlag(mcpToolsSearchCmd, "classification", "Filter by classification (repeatable): "+toolClassificationFlag.usage())
	addPaginationFlags(mcpToolsSearchCmd)
	markRequired(mcpToolsSearchCmd, "app-id", "connector-id")
	mcpToolsCmd.AddCommand(mcpToolsSearchCmd)
}
