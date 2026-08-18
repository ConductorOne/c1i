package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpServersResyncToolsCmd = &cobra.Command{
	Use:   "resync-tools <connector-id>",
	Short: "Re-run tool discovery for an MCP server",
	Long: `Re-run tool discovery against the MCP server using the calling user's own
credential. Newly discovered tools land in PENDING_REVIEW; approve them with
"c1i mcp tools approve".`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id"); err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		appID, _ := cmd.Flags().GetString("app-id")
		connectorID := args[0]

		body := map[string]any{
			"appId":       appID,
			"connectorId": connectorID,
		}
		path := client.Path("/api/v1/apps/%s/mcp_servers/%s/resync_tools", appID, connectorID)
		if dryRunActive() {
			return printDryRun(cmd, "POST", path, body)
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		if _, err := c.Post(cmd.Context(), path, body); err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Triggered tool resync: connector_id=%s\n", connectorID)
		return nil
	},
}

func init() {
	mcpServersResyncToolsCmd.Flags().String("app-id", "", "Application ID")
	markRequired(mcpServersResyncToolsCmd, "app-id")
	mcpServersCmd.AddCommand(mcpServersResyncToolsCmd)
}
