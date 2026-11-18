package cmd

import "github.com/spf13/cobra"

var mcpPromptsCmd = &cobra.Command{
	Use:   "prompts",
	Short: "Review MCP prompts discovered from a server",
	Long: `List, search, get, approve and view history for the MCP prompts a
connector's MCP server exposes. Prompt metadata comes from discovery; the API
spec says "Only state is editable", so the only change c1i can make is the
approval state, with "mcp prompts approve".

A tenant without the prompts feature answers a well-formed prompts request
with 403 "feature not enabled" (exit 3); a malformed id gets the server's 400
first (exit 2).`,
}

var promptItems = mcpItemKind{
	noun: "prompt", segment: "mcp_prompts", envelope: "prompt", state: promptStateFlag, historyMax: maxPageSize,
	removedAdvice: "use --state=disabled to block it",
}

var promptStateFlag = enumFlag{flag: "state", prefix: "MCP_PROMPT_STATE_", unknown: enumPassPrefixed, values: []enumValue{
	{name: "pending", api: "MCP_PROMPT_STATE_PENDING_REVIEW"},
	{name: "approved", api: "MCP_PROMPT_STATE_APPROVED"},
	{name: "disabled", api: "MCP_PROMPT_STATE_DISABLED"},
	{name: "removed", api: "MCP_PROMPT_STATE_REMOVED"},
}}

var mcpPromptsGetCmd = promptItems.getCmd("Get a single MCP prompt (pretty JSON)",
	`Get one MCP prompt's stored metadata and its argument definitions, in
discovery order. The prompt is not rendered.`)

var mcpPromptsHistoryCmd = promptItems.historyCmd("Stream the change history for a single MCP prompt (NDJSON output)",
	`One NDJSON record per historical version of the prompt, newest first as the
server returns them: a snapshot of the prompt plus change metadata.`)

var mcpPromptsApproveCmd = promptItems.approveCmd("Approve one or more MCP prompts (sets state=APPROVED)",
	`Approve MCP prompts by setting each one's state to MCP_PROMPT_STATE_APPROVED,
or to another state with --state (disabled, pending). The API spec says "Only
state is editable", so there is no separate update command. It also says
"Approval changes converge asynchronously to access grants; success confirms
the saved state."

Pass one or more prompt ids; all share --app-id, --connector-id and --state.
Each id is its own request and prints one compact JSON line (the prompt, .id
and .state) on stdout, in argument order. If one fails the rest are still
attempted and the command exits with the first failure's code, so it can exit
non-zero with some prompts already changed: under --error-format json check
each per-id error's "applied" and the summary's "notAttempted".

--state=removed is refused (exit 2, nothing sent): sync sets it when a prompt
disappears from the upstream server.`)

func init() {
	mcpCmd.AddCommand(mcpPromptsCmd)
	promptItems.registerGet(mcpPromptsCmd, mcpPromptsGetCmd)
	promptItems.registerHistory(mcpPromptsCmd, mcpPromptsHistoryCmd)
	promptItems.registerApprove(mcpPromptsCmd, mcpPromptsApproveCmd)
}
