package cmd

import "github.com/spf13/cobra"

var mcpResourcesCmd = &cobra.Command{
	Use:   "resources",
	Short: "Review MCP resources discovered from a server",
	Long: `List, search, get, approve and view history for the MCP resources a
connector's MCP server exposes. Resources are discovered by sync; their
metadata is read-only ("Resource metadata is discovery-owned and read-only",
API spec), so the only change c1i can make is the approval state, with
"mcp resources approve".`,
}

var resourceItems = mcpItemKind{
	noun: "resource", segment: "mcp_resources", envelope: "resource", state: resourceStateFlag, historyMax: maxHistoryPageSize,
	removedAdvice: "use --state=disabled to block it",
}

var resourceStateFlag = enumFlag{flag: "state", prefix: "MCP_RESOURCE_STATE_", unknown: enumPassPrefixed, values: []enumValue{
	{name: "pending", api: "MCP_RESOURCE_STATE_PENDING_REVIEW"},
	{name: "approved", api: "MCP_RESOURCE_STATE_APPROVED"},
	{name: "disabled", api: "MCP_RESOURCE_STATE_DISABLED"},
	{name: "removed", api: "MCP_RESOURCE_STATE_REMOVED"},
}}

var resourceKindFlag = enumFlag{flag: "kind", prefix: "MCP_RESOURCE_KIND_", unknown: enumPassPrefixed, values: []enumValue{
	{name: "static", api: "MCP_RESOURCE_KIND_STATIC"},
	{name: "template", api: "MCP_RESOURCE_KIND_TEMPLATE"},
}}

var mcpResourcesGetCmd = resourceItems.getCmd("Get a single MCP resource (pretty JSON)",
	`Get one MCP resource: its approval state, kind, URI or URI template, MIME
type and bound app_entitlement_id.`)

var mcpResourcesHistoryCmd = resourceItems.historyCmd("Stream the change history for a single MCP resource (NDJSON output)",
	`One NDJSON record per historical version of the resource, newest first as
the server returns them: {"snapshot": <the resource>, "metadata": {actor,
changeKind, createdAt, traceId, ...}}. An unknown resource id prints nothing
and exits 0 (the server answers with an empty list), as "mcp tools history"
does.`)

var mcpResourcesApproveCmd = resourceItems.approveCmd("Approve one or more MCP resources (sets state=APPROVED)",
	`Approve MCP resources by setting each one's state to
MCP_RESOURCE_STATE_APPROVED, or to another state with --state (disabled,
pending). State is the only field the API lets you change on a resource, so
there is no separate update command.

Pass one or more resource ids; all share --app-id, --connector-id and --state.
Each id is its own request and prints one compact JSON line (the resource,
.id and .state) on stdout, in argument order. If one fails the rest are still
attempted and the command exits with the first failure's code, so it can exit
non-zero with some resources already changed: under --error-format json check
each per-id error's "applied" and the summary's "notAttempted".

--state=removed is refused (exit 2, nothing sent): sync sets it when a
resource disappears from the upstream server.

  c1i mcp resources approve $(c1i mcp resources search --app-id A \
    --connector-id C --state pending --fields id | jq -r .id) \
    --app-id A --connector-id C`)

func init() {
	mcpCmd.AddCommand(mcpResourcesCmd)
	resourceItems.registerGet(mcpResourcesCmd, mcpResourcesGetCmd)
	resourceItems.registerHistory(mcpResourcesCmd, mcpResourcesHistoryCmd)
	resourceItems.registerApprove(mcpResourcesCmd, mcpResourcesApproveCmd)
}
