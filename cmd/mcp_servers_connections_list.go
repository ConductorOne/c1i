package cmd

import (
	"github.com/spf13/cobra"
)

var mcpServersConnectionsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the calling user's per-user MCP connections (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		// Rows carry no id of their own, so only the token and idle-page checks apply.
		return listPages(cmd, c, pageSpec[connectionView]{
			Route: "GET /api/v1/mcp_server_connections",
			Fetch: getPage(cmd.Context(), "/api/v1/mcp_server_connections", nil),
			Parse: parseList[connectionView],
			Rows:  rowFn(connectionRow),
		})
	},
}

func init() {
	addPaginationFlags(mcpServersConnectionsListCmd)
	mcpServersConnectionsCmd.AddCommand(mcpServersConnectionsListCmd)
}
