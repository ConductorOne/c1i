package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpToolsetsDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Soft-delete an MCP toolset",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id", "connector-id", "id"); err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		appID, _ := cmd.Flags().GetString("app-id")
		connectorID, _ := cmd.Flags().GetString("connector-id")
		id, _ := cmd.Flags().GetString("id")

		path := client.Path("/api/v1/apps/%s/connectors/%s/mcp_toolsets/%s", appID, connectorID, id)
		if dryRunActive() {
			return printDryRun(cmd, "DELETE", path, nil)
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		if _, err := c.Delete(cmd.Context(), path); err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Deleted toolset: id=%s\n", id)
		return nil
	},
}

func init() {
	mcpToolsetsDeleteCmd.Flags().String("app-id", "", "Application ID")
	mcpToolsetsDeleteCmd.Flags().String("connector-id", "", "Connector ID")
	mcpToolsetsDeleteCmd.Flags().String("id", "", "MCP toolset ID")
	markRequired(mcpToolsetsDeleteCmd, "app-id", "connector-id", "id")
	mcpToolsetsCmd.AddCommand(mcpToolsetsDeleteCmd)
}
