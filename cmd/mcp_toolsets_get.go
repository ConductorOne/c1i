package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpToolsetsGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Get a single MCP toolset (pretty JSON)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id", "connector-id", "id"); err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := client.New(cmd.Context(), baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		appID, _ := cmd.Flags().GetString("app-id")
		connectorID, _ := cmd.Flags().GetString("connector-id")
		id, _ := cmd.Flags().GetString("id")

		path := fmt.Sprintf("/api/v1/apps/%s/connectors/%s/mcp_toolsets/%s", appID, connectorID, id)
		data, err := c.Get(cmd.Context(), path, nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeObject(cmd, data)
	},
}

func init() {
	mcpToolsetsGetCmd.Flags().String("app-id", "", "Application ID")
	mcpToolsetsGetCmd.Flags().String("connector-id", "", "Connector ID")
	mcpToolsetsGetCmd.Flags().String("id", "", "MCP toolset ID")
	markRequired(mcpToolsetsGetCmd, "app-id", "connector-id", "id")
	mcpToolsetsCmd.AddCommand(mcpToolsetsGetCmd)
}
