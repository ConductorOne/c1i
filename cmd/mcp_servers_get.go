package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpServersGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Get a single MCP server (pretty JSON)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id", "connector-id"); err != nil {
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
		connectorID, _ := cmd.Flags().GetString("connector-id")

		path := client.Path("/api/v1/apps/%s/mcp_servers/%s", appID, connectorID)
		data, err := c.Get(cmd.Context(), path, nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeObject(cmd, data)
	},
}

func init() {
	mcpServersGetCmd.Flags().String("app-id", "", "Application ID")
	mcpServersGetCmd.Flags().String("connector-id", "", "MCP server (connector) ID")
	markRequired(mcpServersGetCmd, "app-id", "connector-id")
	mcpServersCmd.AddCommand(mcpServersGetCmd)
}
