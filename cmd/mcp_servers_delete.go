package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpServersDeleteCmd = &cobra.Command{
	Use:   "delete <connector-id>",
	Short: "Soft-delete an MCP server",
	Long: `Soft-delete an MCP server. Stops the connector and removes it from the
tenant's MCP catalog; the underlying Connector record is retained for audit,
and any per-user OAuth credentials issued against it are revoked.

This cascades: every toolset bound to the connector, and each toolset's app
entitlement, is soft-deleted with it. Anyone whose access came through one of
those entitlements is affected, so check what depends on the server first, e.g.
"c1i mcp toolsets list --app-id X --connector-id Y" and "c1i grants list
--app-id X --entitlement-id Z".

Prints the API's response, which is {}; exit 0 is the success signal. Honors
--dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id"); err != nil {
			return err
		}

		appID, _ := cmd.Flags().GetString("app-id")
		connectorID := args[0]

		path := client.Path("/api/v1/apps/%s/mcp_servers/%s", appID, connectorID)
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: path})
	},
}

func init() {
	mcpServersDeleteCmd.Flags().String("app-id", "", "Application ID")
	markRequired(mcpServersDeleteCmd, "app-id")
	markMutating(mcpServersDeleteCmd)
	mcpServersCmd.AddCommand(mcpServersDeleteCmd)
}
