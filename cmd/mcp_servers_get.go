package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpServersGetCmd = &cobra.Command{
	Use:   "get <connector-id>",
	Short: "Get a single MCP server (pretty JSON)",
	Long: `Get a single MCP server by its connector ID.

Output is the server object itself, not the API's "mcpServer" wrapper. An MCP
server has no "id" field at all -- its identity is "connectorId", the same
value this command takes as its argument and the one "mcp servers list" rows
emit as connector_id. So read "jq -r .connectorId" here, not ".id".`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id"); err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		appID, _ := cmd.Flags().GetString("app-id")
		connectorID := args[0]

		path := client.Path("/api/v1/apps/%s/mcp_servers/%s", appID, connectorID)
		data, err := c.Get(cmd.Context(), path, nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeResource(cmd, data, "connectorId")
	},
}

func init() {
	mcpServersGetCmd.Flags().String("app-id", "", "Application ID")
	markRequired(mcpServersGetCmd, "app-id")
	mcpServersCmd.AddCommand(mcpServersGetCmd)
}
