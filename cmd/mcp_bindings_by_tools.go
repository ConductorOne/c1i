package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpBindingsByToolsCmd = &cobra.Command{
	Use:   "by-tools",
	Short: "List the toolsets each given tool is bound to (NDJSON output)",
	Long: `Reverse lookup: given a batch of MCP tool IDs (--tool-id, repeatable, max 32),
emit one NDJSON record per tool with the toolsets it is bound to. Tools with
no bindings are still emitted with an empty toolsets array.`,
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
		toolIDs, err := repeatableStringFlag(cmd, "tool-id")
		if err != nil {
			return err
		}
		if len(toolIDs) == 0 {
			return &usageError{fmt.Errorf("flag --tool-id requires at least one value")}
		}

		body := map[string]any{
			"appId":       appID,
			"connectorId": connectorID,
			"mcpToolIds":  toolIDs,
		}

		// Validate flags before paying for a client (mirrors create/delete):
		// no reason to build one just to reject the request afterward.
		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		path := client.Path("/api/v1/apps/%s/connectors/%s/tool_bindings/by_tools", appID, connectorID)
		data, err := c.Post(cmd.Context(), path, body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		var resp struct {
			AccessProfilesForTools []struct {
				MCPToolID      string        `json:"mcpToolId"`
				AccessProfiles []toolsetView `json:"accessProfiles"`
			} `json:"accessProfilesForTools"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		enc := newEmitter(cmd)
		for _, group := range resp.AccessProfilesForTools {
			toolsets := make([]map[string]any, 0, len(group.AccessProfiles))
			for _, p := range group.AccessProfiles {
				toolsets = append(toolsets, toolsetRow(p))
			}
			_ = enc.Encode(map[string]any{
				"mcp_tool_id": group.MCPToolID,
				"toolsets":    toolsets,
			})
		}
		return nil
	},
}

func init() {
	mcpBindingsByToolsCmd.Flags().String("app-id", "", "Application ID")
	mcpBindingsByToolsCmd.Flags().String("connector-id", "", "Connector ID")
	addRepeatableStringFlag(mcpBindingsByToolsCmd, "tool-id", "MCP tool ID to look up (repeatable; max 32)")
	markRequired(mcpBindingsByToolsCmd, "app-id", "connector-id", "tool-id")
	mcpBindingsCmd.AddCommand(mcpBindingsByToolsCmd)
}
