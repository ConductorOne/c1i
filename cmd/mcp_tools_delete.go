package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpToolsDeleteCmd = &cobra.Command{
	Use:   "delete <tool-id>",
	Short: "Soft-delete an MCP tool",
	Long: `Soft-delete an MCP tool. The tool stays in the database (for audit) but is
hidden from listings and unbound from any toolsets.

Note: tools normally transition to MCP_TOOL_STATE_REMOVED automatically during
sync when they disappear from the upstream MCP server. Use "mcp tools approve
--state=disabled" to block a tool without deleting it.

Prints the API's response, which is {}; exit 0 is the success signal. Honors
--dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		appID, _ := cmd.Flags().GetString("app-id")
		connectorID, _ := cmd.Flags().GetString("connector-id")
		id := args[0]

		path := client.Path("/api/v1/apps/%s/connectors/%s/mcp_tools/%s", appID, connectorID, id)
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: path})
	},
}

func init() {
	mcpToolsDeleteCmd.Flags().String("app-id", "", "Application ID")
	mcpToolsDeleteCmd.Flags().String("connector-id", "", "Connector ID")
	markRequired(mcpToolsDeleteCmd, "app-id", "connector-id")
	markMutating(mcpToolsDeleteCmd)
	mcpToolsCmd.AddCommand(mcpToolsDeleteCmd)
}
