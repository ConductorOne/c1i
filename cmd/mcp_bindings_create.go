package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpBindingsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Bind one or more MCP tools to a toolset (pretty JSON)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id", "connector-id", "toolset-id"); err != nil {
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
		toolsetID, _ := cmd.Flags().GetString("toolset-id")
		toolIDs, _ := cmd.Flags().GetStringSlice("tool-id")
		if len(toolIDs) == 0 {
			return fmt.Errorf("flag --tool-id requires at least one value")
		}

		body := map[string]any{
			"appId":           appID,
			"connectorId":     connectorID,
			"accessProfileId": toolsetID,
			"mcpToolIds":      toolIDs,
		}

		path := client.Path("/api/v1/apps/%s/connectors/%s/mcp_toolsets/%s/tool_bindings", appID, connectorID, toolsetID)
		if dryRunActive() {
			return printDryRun(cmd, "POST", path, body)
		}
		data, err := c.Post(cmd.Context(), path, body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeRawObject(cmd, data)
	},
}

func init() {
	mcpBindingsCreateCmd.Flags().String("app-id", "", "Application ID")
	mcpBindingsCreateCmd.Flags().String("connector-id", "", "Connector ID")
	mcpBindingsCreateCmd.Flags().String("toolset-id", "", "MCP toolset (access profile) ID")
	mcpBindingsCreateCmd.Flags().StringSlice("tool-id", nil, "MCP tool ID to bind (repeatable; max 100)")
	markRequired(mcpBindingsCreateCmd, "app-id", "connector-id", "toolset-id", "tool-id")
	mcpBindingsCmd.AddCommand(mcpBindingsCreateCmd)
}
