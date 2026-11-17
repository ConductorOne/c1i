package cmd

import (
	"context"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpServersListCmd = &cobra.Command{
	Use:   "list",
	Short: "List MCP servers registered under an app (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {

		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		appID, _ := cmd.Flags().GetString("app-id")
		s := serverListSpec(cmd.Context(), appID)
		s.Rows = rowFn(serverRow)
		return listPages(cmd, c, s)
	},
}

// serverListSpec pages GET /api/v1/apps/{app}/mcp_servers.
func serverListSpec(ctx context.Context, appID string) pageSpec[serverView] {
	path := client.Path("/api/v1/apps/%s/mcp_servers", appID)
	return pageSpec[serverView]{
		Route: "GET " + path,
		Fetch: getPage(ctx, path, nil),
		Parse: parseList[serverView],
		ID:    func(it serverView) string { return it.ConnectorID },
	}
}

func init() {
	mcpServersListCmd.Flags().String("app-id", "", "Application ID")
	addPaginationFlags(mcpServersListCmd)
	markRequired(mcpServersListCmd, "app-id")
	mcpServersCmd.AddCommand(mcpServersListCmd)
}
