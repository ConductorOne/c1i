package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpBindingsDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Unbind one or more MCP tools from a toolset",
	Long: `Remove one or more MCP tools (--tool-id, repeatable) from a toolset.

Uses the POST .../tool_bindings/delete action route because the tool IDs travel
in the request body; HTTP DELETE doesn't reliably support that.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id", "connector-id", "toolset-id"); err != nil {
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

		path := fmt.Sprintf("/api/v1/apps/%s/connectors/%s/mcp_toolsets/%s/tool_bindings/delete", appID, connectorID, toolsetID)
		if _, err := c.Post(cmd.Context(), path, body); err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		out := map[string]any{
			"unbound":    len(toolIDs),
			"toolset_id": toolsetID,
			"tool_ids":   toolIDs,
		}
		_ = json.NewEncoder(cmd.OutOrStdout()).Encode(out)
		return nil
	},
}

func init() {
	mcpBindingsDeleteCmd.Flags().String("app-id", "", "Application ID")
	mcpBindingsDeleteCmd.Flags().String("connector-id", "", "Connector ID")
	mcpBindingsDeleteCmd.Flags().String("toolset-id", "", "MCP toolset (access profile) ID")
	mcpBindingsDeleteCmd.Flags().StringSlice("tool-id", nil, "MCP tool ID to unbind (repeatable; max 100)")
	markRequired(mcpBindingsDeleteCmd, "app-id", "connector-id", "toolset-id", "tool-id")
	mcpBindingsCmd.AddCommand(mcpBindingsDeleteCmd)
}
