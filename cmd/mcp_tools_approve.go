package cmd

var mcpToolsApproveCmd = toolItems.approveCmd("Approve one or more MCP tools (sets state=APPROVED)",
	`Approve MCP tools by setting each tool's state to MCP_TOOL_STATE_APPROVED.

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
classification, visibility).`)

func init() { toolItems.registerApprove(mcpToolsCmd, mcpToolsApproveCmd) }
