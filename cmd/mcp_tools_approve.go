package cmd

import (
	"fmt"
	"net/http"

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
its own request and prints one compact JSON line (the tool, .id and .state) per
id on stdout, in argument order, even for a single id. If one fails the rest are
still attempted and the command exits with the first failure's code, so it can
exit non-zero with some tools already approved: under --error-format json check
each per-id error's "applied" and the summary's "notAttempted". A failed stdout
write aborts the batch. All ids share --app-id,
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
		appID, _ := cmd.Flags().GetString("app-id")
		connectorID, _ := cmd.Flags().GetString("connector-id")
		state, err := toolStateFlag.parse(mustGetString(cmd, "state"))
		if err != nil {
			return err
		}
		if state == "" {
			state = "MCP_TOOL_STATE_APPROVED"
		}
		// MCP_TOOL_STATE_REMOVED is reserved for sync's "no longer exists in
		// upstream MCP server" soft-delete signal. Letting an admin set it by
		// hand contaminates that meaning — block it here.
		if state == "MCP_TOOL_STATE_REMOVED" {
			return &usageError{fmt.Errorf("--state=removed is system-managed by sync; use \"mcp tools delete\" to soft-delete a tool, or --state=disabled to block it")}
		}

		ms := make([]mutation, len(args))
		for i, id := range args {
			ms[i] = mutation{
				Method: http.MethodPost,
				Path:   client.Path("/api/v1/apps/%s/connectors/%s/mcp_tools/%s", appID, connectorID, id),
				Body: map[string]any{
					"tool": map[string]any{
						"id":          id,
						"appId":       appID,
						"connectorId": connectorID,
						"state":       state,
					},
					"updateMask": "state",
				},
			}
		}
		return sendMutationBatch(cmd, ms, "id", func(i int) string { return args[i] }, withBatchWording("approved", "tool"))
	},
}

func init() {
	mcpToolsApproveCmd.Flags().String("app-id", "", "Application ID")
	mcpToolsApproveCmd.Flags().String("connector-id", "", "Connector ID")
	mcpToolsApproveCmd.Flags().String("state", "", "Target state (default approved): "+toolStateFlag.without("removed").usage())
	markRequired(mcpToolsApproveCmd, "app-id", "connector-id")
	markMutating(mcpToolsApproveCmd)
	mcpToolsCmd.AddCommand(mcpToolsApproveCmd)
}
