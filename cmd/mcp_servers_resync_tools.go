package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpServersResyncToolsCmd = &cobra.Command{
	Use:   "resync-tools <connector-id>",
	Short: "Re-run tool discovery for an MCP server",
	Long: `Re-run tool discovery against the MCP server using the calling user's own
credential. Newly discovered tools land in PENDING_REVIEW; approve them with
"c1i mcp tools approve".

EXTERNAL servers only; HOSTED servers return an error ("resync is only supported
for external MCP servers").

Prints the API's response, which is {}; exit 0 is the success signal. Honors
--dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id"); err != nil {
			return err
		}

		appID, _ := cmd.Flags().GetString("app-id")
		connectorID := args[0]

		body := map[string]any{
			"appId":       appID,
			"connectorId": connectorID,
		}
		path := client.Path("/api/v1/apps/%s/mcp_servers/%s/resync_tools", appID, connectorID)
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: body})
	},
}

func init() {
	mcpServersResyncToolsCmd.Flags().String("app-id", "", "Application ID")
	markRequired(mcpServersResyncToolsCmd, "app-id")
	markMutating(mcpServersResyncToolsCmd)
	mcpServersCmd.AddCommand(mcpServersResyncToolsCmd)
}
