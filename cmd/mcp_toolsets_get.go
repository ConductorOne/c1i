package cmd

import (
	"bytes"
	"encoding/json"
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

		path := client.Path("/api/v1/apps/%s/connectors/%s/mcp_toolsets/%s", appID, connectorID, id)
		data, err := c.Get(cmd.Context(), path, nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		var pretty bytes.Buffer
		if err := json.Indent(&pretty, data, "", "  "); err != nil {
			return fmt.Errorf("failed to pretty-print response: %w", err)
		}
		_, _ = pretty.WriteTo(cmd.OutOrStdout())
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		return nil
	},
}

func init() {
	mcpToolsetsGetCmd.Flags().String("app-id", "", "Application ID")
	mcpToolsetsGetCmd.Flags().String("connector-id", "", "Connector ID")
	mcpToolsetsGetCmd.Flags().String("id", "", "MCP toolset ID")
	markRequired(mcpToolsetsGetCmd, "app-id", "connector-id", "id")
	mcpToolsetsCmd.AddCommand(mcpToolsetsGetCmd)
}
