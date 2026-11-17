package cmd

import (
	"encoding/json"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpToolsHistoryCmd = &cobra.Command{
	Use:   "history <tool-id>",
	Short: "Stream the change history for a single MCP tool (NDJSON output)",
	Long: `One NDJSON record per historical version of the tool, newest-first as
the server returns them. The record carries both the full tool snapshot at
that point in time and the change-history envelope (actor, change_kind,
created_at, trace_id, syslog_event_id, annotations).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		appID, _ := cmd.Flags().GetString("app-id")
		connectorID, _ := cmd.Flags().GetString("connector-id")
		id := args[0]
		path := client.Path("/api/v1/apps/%s/connectors/%s/mcp_tools/%s/history", appID, connectorID, id)
		// Rows carry no id of their own, so only the token and idle-page checks apply.
		return listPages(cmd, c, pageSpec[json.RawMessage]{
			Route: "GET " + path,
			Fetch: getPage(cmd.Context(), path, nil),
			Parse: parseList[json.RawMessage],
		})
	},
}

func init() {
	mcpToolsHistoryCmd.Flags().String("app-id", "", "Application ID")
	mcpToolsHistoryCmd.Flags().String("connector-id", "", "Connector ID")
	addPaginationFlagsWithMax(mcpToolsHistoryCmd, defaultPageSize, maxHistoryPageSize)
	markRequired(mcpToolsHistoryCmd, "app-id", "connector-id")
	mcpToolsCmd.AddCommand(mcpToolsHistoryCmd)
}
