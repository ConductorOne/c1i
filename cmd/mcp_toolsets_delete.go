package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpToolsetsDeleteCmd = &cobra.Command{
	Use:   "delete <toolset-id>",
	Short: "Soft-delete an MCP toolset",
	Long: `Soft-delete an MCP toolset. Prints the API's response, which is {}; exit 0 is
the success signal. Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		appID, _ := cmd.Flags().GetString("app-id")
		connectorID, _ := cmd.Flags().GetString("connector-id")
		id := args[0]

		path := client.Path("/api/v1/apps/%s/connectors/%s/mcp_toolsets/%s", appID, connectorID, id)
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: path})
	},
}

func init() {
	mcpToolsetsDeleteCmd.Flags().String("app-id", "", "Application ID")
	mcpToolsetsDeleteCmd.Flags().String("connector-id", "", "Connector ID")
	markRequired(mcpToolsetsDeleteCmd, "app-id", "connector-id")
	markMutating(mcpToolsetsDeleteCmd)
	mcpToolsetsCmd.AddCommand(mcpToolsetsDeleteCmd)
}
