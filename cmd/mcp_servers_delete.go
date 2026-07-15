package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpServersDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Soft-delete an MCP server",
	Long: `Soft-delete an MCP server. Stops the connector and removes it from the
tenant's MCP catalog; the underlying Connector record is retained for audit,
and any per-user OAuth credentials issued against it are revoked.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id", "connector-id"); err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		appID, _ := cmd.Flags().GetString("app-id")
		connectorID, _ := cmd.Flags().GetString("connector-id")

		path := client.Path("/api/v1/apps/%s/mcp_servers/%s", appID, connectorID)
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

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Deleted MCP server: connector_id=%s\n", connectorID)
		return nil
	},
}

func init() {
	mcpServersDeleteCmd.Flags().String("app-id", "", "Application ID")
	mcpServersDeleteCmd.Flags().String("connector-id", "", "MCP server (connector) ID")
	markRequired(mcpServersDeleteCmd, "app-id", "connector-id")
	mcpServersCmd.AddCommand(mcpServersDeleteCmd)
}
