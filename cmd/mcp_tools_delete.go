package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpToolsDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Soft-delete an MCP tool",
	Long: `Soft-delete an MCP tool. The tool stays in the database (for audit) but is
hidden from listings and unbound from any toolsets.

Note: tools normally transition to MCP_TOOL_STATE_REMOVED automatically during
sync when they disappear from the upstream MCP server. Use "mcp tools approve
--state=disabled" to block a tool without deleting it.`,
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

		path := fmt.Sprintf("/api/v1/apps/%s/connectors/%s/mcp_tools/%s", appID, connectorID, id)
		if _, err := c.Delete(cmd.Context(), path); err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Deleted tool: id=%s\n", id)
		return nil
	},
}

func init() {
	mcpToolsDeleteCmd.Flags().String("app-id", "", "Application ID")
	mcpToolsDeleteCmd.Flags().String("connector-id", "", "Connector ID")
	mcpToolsDeleteCmd.Flags().String("id", "", "MCP tool ID")
	markRequired(mcpToolsDeleteCmd, "app-id", "connector-id", "id")
	mcpToolsCmd.AddCommand(mcpToolsDeleteCmd)
}
