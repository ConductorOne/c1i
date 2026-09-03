package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpToolsApproveCmd = &cobra.Command{
	Use:   "approve <tool-id>...",
	Short: "Approve one or more MCP tools (sets state=APPROVED)",
	Long: `Approve MCP tools by setting each tool's state to MCP_TOOL_STATE_APPROVED.

This is the standard tool-approval workflow: newly discovered tools land in
MCP_TOOL_STATE_PENDING_REVIEW and admin approval moves them to APPROVED so the
MCP gateway will proxy calls to them.

Pass one or more tool ids. The API has no batch approve, so each id is sent as
its own request and prints its own confirmation line; if one fails the rest are
still attempted and the command exits non-zero. All ids share --app-id,
--connector-id and --state, so a selector pipes straight in:

  c1i mcp tools approve $(c1i mcp tools search --app-id A --connector-id C \
    --classification read --state pending --fields id | jq -r .id) \
    --app-id A --connector-id C

Use --state to target a different lifecycle state (disabled, pending). The
approve command is a thin wrapper around the underlying Update RPC; use
"c1i api --path=..." directly if you need to update other fields (display name,
classification, visibility).`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		appID, _ := cmd.Flags().GetString("app-id")
		connectorID, _ := cmd.Flags().GetString("connector-id")
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
			return &usageError{fmt.Errorf("--state=removed is system-managed by sync; use \"mcp tools delete\" to soft-delete a tool, or --state=disabled to block it")}
		}

		pathFor := func(id string) string {
			return client.Path("/api/v1/apps/%s/connectors/%s/mcp_tools/%s", appID, connectorID, id)
		}
		bodyFor := func(id string) map[string]any {
			return map[string]any{
				"tool": map[string]any{
					"id":          id,
					"appId":       appID,
					"connectorId": connectorID,
					"state":       state,
				},
				"updateMask": "state",
			}
		}

		if dryRunActive() {
			for _, id := range args {
				if err := printDryRun(cmd, "POST", pathFor(id), bodyFor(id)); err != nil {
					return err
				}
			}
			return nil
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		out := cmd.OutOrStdout()
		approveOne := func(id string) error {
			data, err := c.Post(cmd.Context(), pathFor(id), bodyFor(id))
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
			_, _ = fmt.Fprintf(out, "Updated tool: id=%s state=%s\n", resp.Tool.ID, resp.Tool.State)
			return nil
		}

		var firstErr error
		failed := 0
		for _, id := range args {
			if err := approveOne(id); err != nil {
				failed++
				if firstErr == nil {
					firstErr = err
				}
				// One line per id only when batching; a single id's error is
				// the returned one, so printing it here too would double it.
				if len(args) > 1 {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "tool %s: %v\n", id, err)
				}
			}
		}

		if firstErr != nil {
			if len(args) == 1 {
				return firstErr
			}
			// Wrap the first failure so exit-code classification still fires.
			return fmt.Errorf("approved %d of %d tools; %d failed: %w", len(args)-failed, len(args), failed, firstErr)
		}
		return nil
	},
}

func init() {
	mcpToolsApproveCmd.Flags().String("app-id", "", "Application ID")
	mcpToolsApproveCmd.Flags().String("connector-id", "", "Connector ID")
	mcpToolsApproveCmd.Flags().String("state", "", "Target state (default approved): pending, approved, disabled")
	markRequired(mcpToolsApproveCmd, "app-id", "connector-id")
	mcpToolsCmd.AddCommand(mcpToolsApproveCmd)
}
