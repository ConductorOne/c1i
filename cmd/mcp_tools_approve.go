package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpToolsApproveCmd = &cobra.Command{
	Use:   "approve",
	Short: "Approve an MCP tool (sets state=APPROVED)",
	Long: `Approve an MCP tool by setting its state to MCP_TOOL_STATE_APPROVED.

This is the standard tool-approval workflow: newly discovered tools land in
MCP_TOOL_STATE_PENDING_REVIEW and admin approval moves them to APPROVED so the
MCP gateway will proxy calls to them.

Use --state to target a different lifecycle state (disabled, pending). The
approve command is a thin wrapper around the underlying Update RPC; use
"c1i api --path=..." directly if you need to update other fields (display name,
classification, visibility).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		appID, _ := cmd.Flags().GetString("app-id")
		connectorID, _ := cmd.Flags().GetString("connector-id")
		id, _ := cmd.Flags().GetString("id")
		state, _ := cmd.Flags().GetString("state")
		if state == "" {
			state = "MCP_TOOL_STATE_APPROVED"
		} else {
			state = mapToolState(state)
		}
		// MCP_TOOL_STATE_REMOVED is reserved for sync's "no longer exists in
		// upstream MCP server" soft-delete signal. Letting an admin set it by
		// hand contaminates that meaning — block it here.
		if state == "MCP_TOOL_STATE_REMOVED" {
			return fmt.Errorf("--state=removed is system-managed by sync; use \"mcp tools delete\" to soft-delete a tool, or --state=disabled to block it")
		}

		body := map[string]any{
			"tool": map[string]any{
				"id":          id,
				"appId":       appID,
				"connectorId": connectorID,
				"state":       state,
			},
			"updateMask": "state",
		}

		path := client.Path("/api/v1/apps/%s/connectors/%s/mcp_tools/%s", appID, connectorID, id)
		if dryRunActive() {
			return printDryRun(cmd, "POST", path, body)
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Post(cmd.Context(), path, body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		var resp struct {
			Tool struct {
				ID    string `json:"id"`
				State string `json:"state"`
			} `json:"tool"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Updated tool: id=%s state=%s\n", resp.Tool.ID, resp.Tool.State)
		return nil
	},
}

func init() {
	mcpToolsApproveCmd.Flags().String("app-id", "", "Application ID")
	mcpToolsApproveCmd.Flags().String("connector-id", "", "Connector ID")
	mcpToolsApproveCmd.Flags().String("id", "", "MCP tool ID")
	mcpToolsApproveCmd.Flags().String("state", "", "Target state (default approved): pending, approved, disabled")
	markRequired(mcpToolsApproveCmd, "app-id", "connector-id", "id")
	mcpToolsCmd.AddCommand(mcpToolsApproveCmd)
}
