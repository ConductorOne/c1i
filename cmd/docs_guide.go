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

    c1i mcp tools approve --app-id "$APP_ID" --connector-id "$CONNECTOR_ID" --id "$TOOL_ID"

A tool left in PENDING_REVIEW is never proxied. To explicitly block one
instead of approving it, pass --state disabled to "mcp tools approve".

## 6. Verify

    c1i mcp servers get --app-id "$APP_ID" --connector-id "$CONNECTOR_ID"
    c1i mcp servers search --app-id "$APP_ID" --tool-state approved

If the upstream MCP server later adds tools, re-run discovery without
re-registering:

    c1i mcp servers resync-tools --app-id "$APP_ID" --connector-id "$CONNECTOR_ID"

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

    c1i mcp toolsets get --app-id "$APP_ID" --connector-id "$CONNECTOR_ID" --id "$TOOLSET_ID"

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
    c1i tasks approve --task-id "$TASK_ID"

## 6. Verify

    c1i grants list --app-id "$APP_ID" --entitlement-id "$ENTITLEMENT_ID"

Grants are eventually consistent — a just-created grant can take up to a
minute or two to appear in this list.
`

// guideTestMCPGateway walks through an end-to-end MCP gateway verification:
// register a server, approve its tools, confirm they're actually served by
// the gateway's own handshake, then invoke one. Derived from
// cmd/mcp_servers_register.go, cmd/mcp_tools_approve.go, cmd/grants_list.go,
// and the "mcp gateway list-tools" / "mcp gateway call" commands.
const guideTestMCPGateway = `# Test the MCP gateway

End-to-end verification that a registered MCP server's tools are actually
being served through the MCP gateway: register (or reuse) a server, approve
its tools, confirm the gateway's own handshake lists them, then invoke one.

## 1. Register a server and approve its tools

If you haven't already, see "c1i docs guide register-mcp-server" for the
full walkthrough:

    c1i mcp servers register --app-id "$APP_ID" --type hosted \
      --display-name "My Server" --catalog-id "$CATALOG_ID" --auth none
    c1i mcp tools list --app-id "$APP_ID" --connector-id "$CONNECTOR_ID"
    c1i mcp tools approve --app-id "$APP_ID" --connector-id "$CONNECTOR_ID" --id "$TOOL_ID"

## 2. List the tools the gateway actually exposes

"mcp gateway list-tools" runs the real MCP handshake against the gateway
(not the admin API) and lists the tools it returns — the ground truth for
"is this tool actually callable right now":

    c1i mcp gateway list-tools

Add --full to include each tool's input schema:

    c1i mcp gateway list-tools --full

Only APPROVED tools the caller has been granted show up here. If a tool you
just approved is missing, double check its state ("c1i mcp tools get") and
the caller's grant on its toolset entitlement ("c1i grants list --app-id
"$APP_ID" --entitlement-id "$ENTITLEMENT_ID"") before assuming the gateway
itself is broken.

## 3. Call a tool through the gateway

    c1i mcp gateway call "<tool-name>" --args '{"key":"value"}'

"<tool-name>" is the tool_name from "mcp gateway list-tools" (or "mcp tools
list"), not the internal tool ID. Omit --args for a tool that takes no
input.

## Gateway URL and auth

The gateway URL is derived from --url / C1I_URL by inserting "-mcp" into the
host — e.g. acme.conductor.one becomes acme-mcp.conductor.one/v1. Override it
directly with --gateway-url if your tenant's gateway is hosted elsewhere.
Both "list-tools" and "call" authenticate with the same stored c1i
credentials as every other command — no separate MCP-specific auth step.
`

// docsGuides maps a guide name to its embedded content. Keep names stable —
// they're part of the CLI's public surface (an agent may hardcode
// "c1i docs guide register-mcp-server" in its own tooling).
var docsGuides = map[string]string{
	"register-mcp-server":     guideRegisterMCPServer,
	"assign-toolset-everyone": guideAssignToolsetEveryone,
	"test-mcp-gateway":        guideTestMCPGateway,
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
