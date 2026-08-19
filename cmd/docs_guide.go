package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// guideRegisterMCPServer walks through registering a HOSTED or EXTERNAL MCP
// server and approving its discovered tools. Derived from cmd/mcp_servers_register.go,
// cmd/mcp_servers_register_template.go, cmd/mcp_servers_test_connection.go,
// cmd/mcp_tools_approve.go, and cmd/mcp_servers_resync_tools.go.
const guideRegisterMCPServer = `# Register an MCP server

End-to-end walkthrough for registering a new MCP server (HOSTED or EXTERNAL)
under a C1 app, then approving its discovered tools so the MCP gateway will
proxy calls to them. Every step below (other than "docs" commands) requires
authentication — see "c1i auth login".

## 1. Get an app to register under

MCP servers are modeled as a connector under an app. Reuse an existing app or
create a new one:

    c1i apps list
    # pick the target app's "id" from the list -> APP_ID
    # or, to start fresh:
    APP_ID=$(c1i apps create --display-name "My MCP Apps" | jq -r .app.id)

## 2a. HOSTED: find a catalog entry

HOSTED servers run inside C1 from a catalog impl. Browse the catalog and note
the entry's "id":

    c1i mcp servers catalog list --query "<name>"
    CATALOG_ID=<id from the entry you want>

Skip to step 3 if you're registering an EXTERNAL server instead.

## 2b. EXTERNAL: probe reachability first

Before registering an EXTERNAL server, confirm it's reachable with your
credentials:

    c1i mcp servers test-connection --url https://your-mcp-server.example/mcp \
      --transport streamable-http --auth bearer-token --bearer-token "$TOKEN"

The response reports "reachable" (bool), "tool_count", and a sanitized
"failure_reason" when it isn't.

## 3. (Optional) Generate a config template for non-trivial auth

The simple auth methods (none, bearer-token, custom-header, basic-auth) can
be passed directly as flags in step 4 — skip this step for those. For OAuth2 /
AWS SigV4 / Google service-account auth, generate a ready-to-edit skeleton
instead of hand-writing the config JSON:

    c1i mcp servers register --print-config-template --auth oauth2 --type hosted > config.json
    # edit config.json, replacing the <placeholders>

Pass the edited file back in step 4 via --hosted-config-file config.json (or
--external-config-file config.json for EXTERNAL).

## 4. Register the server

HOSTED:

    c1i mcp servers register --app-id "$APP_ID" --type hosted \
      --display-name "My Server" --catalog-id "$CATALOG_ID" --auth none

EXTERNAL:

    c1i mcp servers register --app-id "$APP_ID" --type external \
      --display-name "My Server" --url https://your-mcp-server.example/mcp \
      --transport streamable-http --auth bearer-token --bearer-token "$TOKEN"

The command prints the created server as pretty JSON — note its
"connectorId":

    CONNECTOR_ID=<connectorId from the response>

Registering triggers tool discovery automatically; newly discovered tools
land in state MCP_TOOL_STATE_PENDING_REVIEW and are not yet callable.

## 5. Review and approve discovered tools

    c1i mcp tools list --app-id "$APP_ID" --connector-id "$CONNECTOR_ID"

For each tool you want the gateway to proxy:

    c1i mcp tools approve "$TOOL_ID" --app-id "$APP_ID" --connector-id "$CONNECTOR_ID"

A tool left in PENDING_REVIEW is never proxied. To explicitly block one
instead of approving it, pass --state disabled to "mcp tools approve".

## 6. Verify

    c1i mcp servers get "$CONNECTOR_ID" --app-id "$APP_ID"
    c1i mcp servers search --app-id "$APP_ID" --tool-state approved

If the upstream MCP server later adds tools, re-run discovery without
re-registering:

    c1i mcp servers resync-tools "$CONNECTOR_ID" --app-id "$APP_ID"

Next: "c1i docs guide assign-toolset-everyone" groups the approved tools into
a toolset and grants it broadly.
`

// guideAssignToolsetEveryone walks through bundling approved MCP tools into a
// toolset and granting its entitlement to every user. Derived from
// cmd/mcp_toolsets_create.go, cmd/mcp_bindings_create.go,
// cmd/mcp_toolsets_get.go, cmd/users_list.go, cmd/requests_create_grant.go,
// cmd/tasks_approve.go, and cmd/grants_list.go.
const guideAssignToolsetEveryone = `# Assign a toolset to everyone

Group a set of approved MCP tools into a toolset (an admin-curated bundle
bound to a single AppEntitlement) and grant that entitlement to every user in
the tenant. This assumes the server's tools are already APPROVED — see
"c1i docs guide register-mcp-server" first.

NOTE: c1i has no single "grant to everyone" endpoint. This runbook grants the
entitlement to each user individually via the access-request flow; for a
large tenant, drive step 5 from a script over the user IDs from step 4.

## 1. Create the toolset

    c1i mcp toolsets create --app-id "$APP_ID" --connector-id "$CONNECTOR_ID" \
      --display-name "Everyone toolset" --description "Baseline MCP tools for all users"

Note the returned "id":

    TOOLSET_ID=<id from the response>

## 2. Bind approved tools to it

Find the approved tools on this server:

    c1i mcp tools search --app-id "$APP_ID" --connector-id "$CONNECTOR_ID" --state approved

Bind one or more tool IDs to the toolset (--tool-id is repeatable, up to 100
per call):

    c1i mcp bindings create --app-id "$APP_ID" --connector-id "$CONNECTOR_ID" \
      --toolset-id "$TOOLSET_ID" --tool-id "$TOOL_ID_1" --tool-id "$TOOL_ID_2"

## 3. Resolve the toolset's entitlement

Each toolset creates exactly one AppEntitlement at sync time:

    c1i mcp toolsets get "$TOOLSET_ID" --app-id "$APP_ID" --connector-id "$CONNECTOR_ID"

Read appEntitlementId from the response:

    ENTITLEMENT_ID=<appEntitlementId from the response>

If it's empty, the toolset hasn't finished syncing yet — retry after a short
delay.

## 4. Enumerate every user to grant

    c1i users list --status enabled

## 5. Request the entitlement for every user

For each user ID from step 4:

    c1i requests create grant --app-id "$APP_ID" --entitlement-id "$ENTITLEMENT_ID" \
      --user-id "$USER_ID" --description "baseline MCP toolset access"

This files one grant task per user. If the entitlement's access policy
auto-approves, each task resolves on its own. Otherwise, clear the resulting
tasks:

    c1i tasks list --state open --query "<toolset display name>"
    c1i tasks approve "$TASK_ID"

## 6. Verify

    c1i grants list --app-id "$APP_ID" --entitlement-id "$ENTITLEMENT_ID"

Grants are eventually consistent — a just-created grant can take up to a
minute or two to appear in this list.
`

// guideTestMCPGateway verifies, with c1i, the pieces that must be in place
// before a registered MCP server's tools are served through the gateway to
// entitled callers, then drives an actual call through the gateway itself.
// Derived from cmd/mcp_servers_get.go, cmd/mcp_tools_approve.go,
// cmd/mcp_toolsets_get_by_entitlement.go, cmd/grants_list.go,
// cmd/mcp_gateway.go, cmd/mcp_gateway_list_tools.go, and
// cmd/mcp_gateway_call.go.
const guideTestMCPGateway = `# Test the MCP gateway (end-to-end)

Before a registered MCP server's tools can be invoked through the C1 MCP
gateway, several things must line up. This runbook uses c1i to verify each,
then drives an actual tool call through the gateway itself, so you can
localize a "the tool isn't callable" problem to the right layer.
(See "c1i docs guide register-mcp-server" to get a server registered first.)

## 1. The server exists and is configured

    c1i mcp servers get "$CONNECTOR_ID" --app-id "$APP_ID"

Shows the server and its auth/connection state. For an EXTERNAL server, also
confirm the upstream endpoint answers:

    c1i mcp servers test-connection "$CONNECTOR_ID" --app-id "$APP_ID"

## 2. Its tools are discovered and APPROVED

The gateway only proxies APPROVED tools. Newly discovered tools start in
PENDING_REVIEW:

    c1i mcp tools list --app-id "$APP_ID" --connector-id "$CONNECTOR_ID"
    c1i mcp tools get  "$TOOL_ID" --app-id "$APP_ID" --connector-id "$CONNECTOR_ID"
    c1i mcp tools approve "$TOOL_ID" --app-id "$APP_ID" --connector-id "$CONNECTOR_ID"

Note the tool's "tool_name" from either response — the MCP protocol name the
gateway lists it under. That's "$TOOL_NAME" in step 4, and it is a different
value than the "$TOOL_ID" used above.

## 3. The caller holds (or can request) the toolset entitlement

A tool is only exposed to a caller who holds the entitlement of a toolset
that binds it. Resolve the toolset for an entitlement, then check the grant:

    c1i mcp toolsets get-by-entitlement "$AEID" --app-id "$APP_ID"
    c1i grants list --app-id "$APP_ID" --entitlement-id "$ENTITLEMENT_ID"

## 4. Call it through the gateway

With the server registered, the tool approved, and the entitlement granted,
the gateway should now list and serve the tool. List what it exposes to you
(one NDJSON row per tool; --full also prints each tool's input JSON schema):

    c1i mcp gateway list-tools --full

Confirm "$TOOL_NAME" appears, then invoke it. Arguments are a JSON object via
--args (a JSON string, array, or null is rejected as a usage error):

    c1i mcp gateway call "$TOOL_NAME" --args '{"key":"value"}'

The gateway endpoint is derived from --url / C1I_URL by default (inserting
"-mcp" into the host); if the gateway lives elsewhere, override it with
--gateway-url, e.g. "c1i mcp gateway list-tools --gateway-url https://acme-mcp.example.com/v1".

## What this proves

When steps 1-4 check out, the server's approved tools are being served to
entitled callers through the gateway, and "mcp gateway call" proves it end to
end — not just that the pieces are theoretically wired up.
`

// guideDelegateEntitlementProvisioning walks through the two-step needed to
// get real delegated provisioning out of an entitlement proxy binding: the
// binding itself (c1.api.app.v1.AppEntitlementsProxy) is a visibility and
// tracking link only and triggers no provisioning by itself, so a second
// call sets provisionerPolicy.delegated on the destination entitlement.
// Field names and the "binding must already exist" precondition were
// verified against the live public OpenAPI spec schemas
// c1.api.app.v1.AppEntitlementsProxy and c1.api.policy.v1.DelegatedProvision
// ("MUST be configured as a proxy binding leading into this entitlement").
// This is a distinct object from the tool<->toolset bindings under "mcp
// bindings" (cmd/mcp_bindings*.go) — c1i has no dedicated command for
// entitlement proxy bindings, so every step here goes through "c1i api".
// Derived from cmd/api.go and cmd/entitlements_get.go.
const guideDelegateEntitlementProvisioning = `# Delegate provisioning through a proxy binding

An entitlement proxy binding (entitlement -> entitlement) is a different
object from the tool<->toolset bindings the mcp bindings command family
covers. A proxy binding is a visibility and tracking link only — creating
one does not, by itself, trigger any provisioning. Real delegated
provisioning is a separate second step, below.

There is no dedicated command for entitlement proxy bindings, so both steps
below go through "c1i api".

## 1. Create the proxy binding

The binding is directional, and which entitlement goes in which path
position matters — get it backwards and the binding points the wrong way.
Use step 2 below to tell them apart: the destination is the entitlement you
will set provisionerPolicy.delegated ON in step 2; the source is the
entitlement named INSIDE that delegated object — the one whose own connector
actually performs the provisioning. Both ends are identified entirely by the
path; the body is empty:

    c1i api --path=/api/v1/apps/<SRC_APP_ID>/<SRC_ENTITLEMENT_ID>/bindings/<DST_APP_ID>/<DST_ENTITLEMENT_ID> --body='{}'

Confirm it was created:

    c1i api --path=/api/v1/apps/<SRC_APP_ID>/<SRC_ENTITLEMENT_ID>/bindings/<DST_APP_ID>/<DST_ENTITLEMENT_ID> --method=GET

## 2. Turn on delegated provisioning on the destination entitlement

This is the step that actually changes behavior. On the destination
entitlement — the one the binding above leads into — set
provisionerPolicy.delegated, pointing back at the source entitlement:

    c1i entitlements get <DST_ENTITLEMENT_ID> --app-id <DST_APP_ID>

    c1i api --path=/api/v1/apps/<DST_APP_ID>/entitlements/<DST_ENTITLEMENT_ID> --body='{"entitlement":{"provisionerPolicy":{"delegated":{"appId":"<SRC_APP_ID>","entitlementId":"<SRC_ENTITLEMENT_ID>"}}},"updateMask":"provisionerPolicy"}'

C1's schema documents provisionerPolicy.delegated's precondition directly:
the destination entitlement "MUST be configured as a proxy binding leading
into this entitlement" — do step 1 first even though the write API does not
reject the step 2 update if you skip it; whether delegation actually
provisions anything without a real binding behind it is unverified, so
don't rely on that path.

## What this proves

Step 1 alone never grants or provisions anything — it only makes one
entitlement visible and trackable through another. Step 2's
provisionerPolicy.delegated update is the documented provisioning trigger,
and per C1's own schema it's meant to depend on step 1 already being in
place, even though this runbook only confirmed that step 1 succeeds, step 2
succeeds, and the resulting field shape — not that skipping step 1 changes
runtime provisioning behavior.
`

// docsGuides maps a guide name to its embedded content. Keep names stable —
// they're part of the CLI's public surface (an agent may hardcode
// "c1i docs guide register-mcp-server" in its own tooling).
var docsGuides = map[string]string{
	"register-mcp-server":               guideRegisterMCPServer,
	"assign-toolset-everyone":           guideAssignToolsetEveryone,
	"test-mcp-gateway":                  guideTestMCPGateway,
	"delegate-entitlement-provisioning": guideDelegateEntitlementProvisioning,
}

// guideNames returns the available guide names, sorted for stable output.
func guideNames() []string {
	names := make([]string, 0, len(docsGuides))
	for n := range docsGuides {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

var docsGuideCmd = &cobra.Command{
	Use:   "guide [name]",
	Short: "Print an embedded, task-oriented runbook (no auth required)",
	Long: `Print an embedded, task-oriented runbook — a numbered sequence of actual
c1i commands for a common end-to-end workflow.

Run with no argument to list the available guide names. These are static
content embedded in the c1i binary (no network call), unlike "docs search" /
"docs page" which hit the C1 documentation site.

Examples:
  c1i docs guide
  c1i docs guide register-mcp-server`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Available guides:")
			for _, name := range guideNames() {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", name)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "\nRun \"c1i docs guide <name>\" to print one.")
			return nil
		}

		name := args[0]
		content, ok := docsGuides[name]
		if !ok {
			return &usageError{fmt.Errorf("unknown guide %q; available guides: %s", name, strings.Join(guideNames(), ", "))}
		}

		_, _ = fmt.Fprint(cmd.OutOrStdout(), content)
		return nil
	},
}

func init() {
	docsCmd.AddCommand(docsGuideCmd)
}
