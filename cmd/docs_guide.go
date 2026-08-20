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

The response reports "reachable" (bool), "toolCount" (string), and a
sanitized "failureReason" when it isn't.

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
re-registering — EXTERNAL servers only; HOSTED returns a 400:

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

NOTE: this toolset/binding mechanism only governs exposure of tools
discovered from a registered connector — it has no effect on C1's own
built-in c1_* tools, whose gateway exposure tracks the caller's underlying
role and permissions instead.

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

The app must already be configured for self-service access requests (C1 web
console: App > Access requests > standard audience) — c1i has no command for
this. An unconfigured entitlement rejects every request below outright with
403 "target user is not allowed to request that resource", before any task
is filed; matching the entitlement's grantPolicyId to the app's default via
"c1i api" does not fix it.

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

For a tool discovered from a registered connector (HOSTED/EXTERNAL/tunneled
MCP server), exposure requires holding the entitlement of a toolset that
binds it. C1's own built-in c1_* tools are different: they front the C1 API
directly, and their exposure through the gateway tracks the caller's
underlying role and permissions, not any MCP toolset grant — granting or
withholding a toolset has no effect on them. (This is about exposure/listing
only; whether execution of a c1_* tool is similarly unconfined has not been
verified.) Resolve the toolset for an entitlement, then check the grant:

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

A tool result with isError: true exits 7. A JSON-RPC-level failure (the call
itself failed, not the tool) is classified by its code: an unknown tool name
or bad params (-32602) exits 2, since that is caused by what you passed;
method-not-found, parse-error and invalid-request (-32601/-32700/-32600), and
an upstream connector error (code 0 — e.g. an unreachable external MCP server)
exit 8; a JSON-RPC error carrying no code at all, or any other code, exits 1.

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
provisionerPolicy.delegated update is the actual provisioning trigger.
`

// guideConfigureNewApp walks through creating a manually-managed app
// container, setting its owners, and creating a custom entitlement for it
// via the 3-call resource-type/resource/entitlement sequence (no first-class
// "entitlements create" exists). Derived from cmd/apps_create.go,
// cmd/apps_set_owners.go, cmd/entitlements_get.go, cmd/entitlements_list.go,
// and cmd/api.go.
const guideConfigureNewApp = `# Configure a new app

Stand up an app container, assign the C1 users who administer it, and give it
at least one entitlement so it is ready to hand off to an access-granting
workflow. Use this for a manually-managed app with no connector (a physical
badge, a shared credential, a home-grown tool) — not for an app that should
sync real accounts/entitlements from a SaaS connector (Salesforce, Okta,
Google Workspace, ...; see "Common failures") or an MCP server (use "c1i
docs guide register-mcp-server" instead, which creates its own entitlement
via toolset sync).

## Prerequisites

- Pointed at the tenant you intend to change, and authenticated. This guide
  creates objects, so confirm the target before you start — "auth whoami"
  reports the identity but not the tenant:

      c1i auth status
      c1i auth whoami
- At least one candidate owner already exists as a C1 user (owners are
  existing users, never created here — C1 users come from a connected
  directory, not from this or any other write path):

      c1i users list --status enabled

## Steps

### 1. Create the app

    c1i apps create --display-name "Acme Payroll" --description "Manually-managed payroll access"

Prints the created app as pretty JSON under an "app" key. Capture its id:

    APP_ID=<id from the response>

A fresh app already carries one system-builtin entitlement ("Access") and one
default resource type ("Credential") — it is not entirely empty, just empty
of anything specific to what you're about to model.

### 2. Set the app's owners

    OWNER_USER_ID=<id from "c1i users list" above>
    c1i apps set-owners "$APP_ID" --user-id "$OWNER_USER_ID" --wait

"--wait" polls "GET .../ownerids" until the owner appears (or times out) and
prints "Owners provisioned on app ... after ...". Without "--wait", the PUT
returns immediately but the owner takes up to ~60-90s to actually show up
anywhere that reads them back.

### 3. Create a custom entitlement to grant

There is no "entitlements create" — only "entitlements get"/"list". Creating
one for a manually-managed app is a 3-call sequence instead: a resource
type, a resource under it, then the entitlement pointing at both. No
first-class command covers this, so each call goes through "c1i api":

    c1i api --path=/api/v1/apps/$APP_ID/resource_types --body='{"displayName":"Payroll role","resourceType":"CUSTOM"}'
    RT_ID=<appResourceType.id from the response>

    c1i api --path=/api/v1/apps/$APP_ID/resource_types/$RT_ID/resources --body='{"displayName":"Payroll admin"}'
    RES_ID=<appResource.id from the response>

    c1i api --path=/api/v1/apps/$APP_ID/entitlements --body='{"displayName":"Payroll admin","slug":"member","alias":"payroll_admin","appResourceTypeId":"'$RT_ID'","appResourceId":"'$RES_ID'"}'
    ENT_ID=<appEntitlementView.appEntitlement.id from the response>

resourceType is one of ROLE|GROUP|LICENSE|PROJECT|CATALOG|CUSTOM|VAULT|PROFILE_TYPE.
Omitting a duration defaults the entitlement to standing access
(durationUnset); pass a durationGrant field (e.g. 3600s) instead for
time-boxed access.

## Verify

    c1i entitlements list --app-id "$APP_ID"

Auto-paginates to completion; expect the builtin "Access" row plus your new
entitlement, both present immediately — the entitlement search index is not
lagged the way owners are.

    c1i entitlements get "$ENT_ID" --app-id "$APP_ID"

Full object; isManuallyManaged is true.

For owners, don't trust the appOwners field embedded in the app object (from
"c1i apps get <id>") as the source of truth: in testing it stayed empty for
several minutes after "apps set-owners --wait" had already reported success.
Check the raw owner list instead, which is what "--wait" itself polls:

    c1i api --path=/api/v1/apps/$APP_ID/ownerids

Expect your "$OWNER_USER_ID" in userIds within the ~60-90s window (already
satisfied if you used "--wait").

## Common failures

- "apps set-owners" exits 1 with a 400 naming a regex pattern
  (^[a-zA-Z0-9]{27}$) -> the app id or a "--user-id" isn't a real 27-char
  C1 id -> fix the id; retrying as-is won't help.
- "entitlements list --app-id <id>" returns nothing at exit 0 for a
  well-formed but wrong app id -> the search endpoint doesn't validate the
  app exists -> confirm the id with "c1i apps get <id>" (a real 404 there
  exits 4).
- "c1i apps get <id>" on a deleted or never-existed app -> 404 -> exit 4.
- Building a SaaS-backed connector (Salesforce, Okta, Google Workspace, ...)
  by hand through "c1i api" instead of following this runbook -> don't:
  connector "create" needs a provider-specific catalogId that has no
  list/discovery endpoint in c1i, and secrets are write-only once set. That
  path is real but heavy and provider-specific enough that it belongs in its
  own runbook, not bolted onto a generic one.

Next: grant the entitlement you just created:

    c1i requests create grant --app-id "$APP_ID" --entitlement-id "$ENT_ID" --user-id "$USER_ID"

Or see "c1i docs guide delegate-entitlement-provisioning" if access to this
app should flow from another entitlement instead of a direct request.
`

// guideRequestAccess walks the requester side of a grant/revoke access
// request end to end: finding a real app/entitlement/user, previewing with
// --dry-run, filing the request, and verifying the resulting grant.
// Approval mechanics (stepApproverIds, the actions gate, policy.current,
// and the approve/deny asymmetry) are deliberately out of scope — see
// guideInspectAndApproveTask, the single source for those. Derived from
// cmd/requests_create_grant.go, cmd/requests_create_revoke.go,
// cmd/requests_list.go, cmd/grants_list.go, cmd/entitlements_get.go,
// cmd/apps_list.go, and cmd/users_list.go.
const guideRequestAccess = `# Request access

Bind a C1 user to an app entitlement through the request/approve workflow,
confirm who holds it, and take it away again — the only granting path
guaranteed to have a working undo.

Before you request anything: this guide covers access requested through
"requests create grant" / "requests create revoke" only. C1 also supports
binding a user to an entitlement directly, bypassing the request workflow
entirely; c1i has no command for that, and if you reach for it anyway via a
raw API call, know this: the matching direct-removal endpoint only works on
an SSO application's own sign-in entitlement, or on a catalog/group/
profile-type entitlement on the built-in C1 app. For an ordinary role or
custom entitlement on a connector app it refuses outright, and there is no
other undo path. Don't create access that way unless you already know how
you'd take it back.

## Prerequisites

- Authenticated:

      c1i auth whoami

- An app and an entitlement that already exist to request against. There is
  no "entitlements create" — find real ones:

      c1i apps list
      APP_ID=<id of the target app>
      c1i entitlements list --app-id "$APP_ID"
      ENTITLEMENT_ID=<id of the target entitlement>

  Look at the entitlement before requesting it:

      c1i entitlements get "$ENTITLEMENT_ID" --app-id "$APP_ID"

  This is a weak signal, not a guarantee: a populated grantPolicyId does
  not mean the entitlement is requestable, and an empty one does not mean
  it isn't — real requestability is decided by catalog membership, which
  this response does not show and which c1i has no command for reading.
  The create call in step 2 below is the actual test.

- The target's C1 user id — the directory-sourced identity being granted
  access, not an app account ("c1i accounts list"), which is how that same
  identity shows up inside one connected app. A grant binds the user:

      c1i users list --email user@example.com
      USER_ID=<id from the result>

## Steps

1. Preview the request before sending it (no task is created). "--user-id"
   defaults to the caller if omitted; "--dry-run" still authenticates, since
   it resolves self when "--user-id" is omitted:

       c1i requests create grant --app-id "$APP_ID" --entitlement-id "$ENTITLEMENT_ID" --user-id "$USER_ID" --description "<why>" --dry-run

   Confirms the method, path, and body c1i would send.

2. Create the grant request.

       c1i requests create grant --app-id "$APP_ID" --entitlement-id "$ENTITLEMENT_ID" --user-id "$USER_ID" --description "<why>"

   Prints task_id=... state=...; capture it as TASK_ID. A 403 here (target
   user is not allowed to request that resource) means the entitlement
   isn't reachable through any request catalog for this user — a
   console/catalog setting, not something a flag fixes. This is common, not
   exceptional: plenty of real, synced entitlements fail this way.

3. Find the task if you didn't capture its id. As the requester (your own
   opens/subjects):

       c1i requests list --state open

4. Resolve the task. See "c1i docs guide inspect-and-approve-task" for who
   can act on it and the approve/deny/comment mechanics — the requester
   can't approve their own task, so if you requested access for yourself,
   someone else (or an auto-approve policy) has to close it.

5. A revoke request is symmetric, and needs the same resolution:

       c1i requests create revoke --app-id "$APP_ID" --entitlement-id "$ENTITLEMENT_ID" --user-id "$USER_ID" --description "<why>"

   Capture the new task id and resolve it exactly like the grant above.

## Verify

    c1i grants list --app-id "$APP_ID" --entitlement-id "$ENTITLEMENT_ID" --user-id "$USER_ID"

Grants are eventually consistent in both directions: after an approved
grant, expect up to a couple of minutes before the row appears; after an
approved revoke, the same delay before it disappears. An empty result
immediately after approval isn't a failure — wait and re-run the same
command. A revoke task still sitting in TASK_STATE_OPEN past that window
means it's waiting on an approver, not that the revoke failed.

## Common failures

| Symptom | Cause | Fix |
|---|---|---|
| 403 target user is not allowed to request that resource (exit 3) | The entitlement isn't reachable through any request catalog for this user | Configure it in the C1 console (App > Access requests), or target an entitlement that already allows requests |
| 409 duplicate ticket found, with a task id in the error details (exit 1) | An open task for this exact app + entitlement + user already exists | Act on that task id ("tasks list --state open") instead of creating another |
| required flag(s) "app-id", "entitlement-id" not set (exit 2) | Both are required on "requests create grant"/"revoke" | Fix the invocation |
| An auth failure resolving the caller's own id when "--user-id" is omitted (exit 3) | Surfaces before the request call itself runs | Re-authenticate rather than retry as-is |
| "grants list" returns nothing right after approval | Eventual consistency | Wait roughly a minute or two and re-run the same filter |

Approve/deny/comment failures, and everything about stepApproverIds, the
actions gate, and the current policy step, live in
"c1i docs guide inspect-and-approve-task" instead, which is also the next
step for resolving the task you just opened.
`

// guideInspectAndApproveTask is the single source for approver-side task
// mechanics: reading the task's embedded policy (current/next step,
// stepApproverIds, actions), commenting (unlike approve/deny, not gated by
// actions), and the approve/deny step-resolution asymmetry (approve requires
// a resolvable current step; deny proceeds without one). Derived from
// cmd/tasks_list.go, cmd/requests_get.go, cmd/tasks_comment.go,
// cmd/tasks_approve.go, cmd/tasks_deny.go, and cmd/tasks.go
// (resolvePolicyStepID).
const guideInspectAndApproveTask = `# Inspect and approve a task

Read the policy step governing an open access-request task, confirm you're
actually authorized to act on it, then approve, deny, or comment on it.
This does not cover authoring or editing a policy — c1i has no first-class
command for that; only "c1i api" reaches /api/v1/policies (see the policy
gap report).

## Prerequisites

Requires authentication ("c1i auth login"). You need the task's own id —
from a "c1i tasks list" row, a notification, or the output of
"c1i requests create grant"/"revoke".

## Steps

1. Find the task. As an approver — "my work" rather than "my requests":

       c1i tasks list --state open --assigned-to-me
       TASK_ID=<id from the row you're acting on>

   NDJSON; --query matches display name or description.

2. Read the full task view before acting on it. The same endpoint answers
   for any task id, not only ones you created, and it embeds the policy
   currently governing the task — you don't need to look up the
   entitlement or the policy separately:

       c1i requests get "$TASK_ID"

   In the JSON, check:
   - taskView.task.policy.policy.displayName / .id — which policy is
     driving this task. (An entitlement's own grantPolicyId/revokePolicyId
     can be empty — inherited from the app's default (see
     "c1i apps get <app-id>") — so don't infer the governing policy from
     the entitlement alone; the task view always has the resolved one.)
   - taskView.task.policy.current.id — the step this task is on right now.
     This is exactly the value tasks approve/deny send as policyStepId.
   - taskView.task.policy.next — steps still to come. Empty means your
     approval, if it's the last one, closes the task; non-empty means
     another step (often another approver) follows.
   - taskView.task.stepApproverIds — user ids allowed to act on this step.
   - taskView.task.actions — what YOU specifically can do on this task
     right now. If TASK_ACTION_TYPE_APPROVE (or _DENY) isn't listed, don't
     call approve/deny — it will be rejected even though you can read the
     task, and even if you're the requester.

3. Not authorized to approve or deny? Comment instead. Unlike approve/deny,
   commenting is not gated by stepApproverIds/actions — it succeeds for any
   authenticated caller regardless of step:

       c1i tasks comment "$TASK_ID" --comment "note for the approver"

4. Approve or deny:

       c1i tasks approve "$TASK_ID" --comment "reviewed, looks correct"

   Or:

       c1i tasks deny "$TASK_ID" --comment "not needed"

   Both resolve --policy-step-id to the current step automatically (step
   2's policy.current.id) — pass it explicitly only if that lookup fails or
   you deliberately want a non-current step. tasks approve errors if no
   current step can be resolved (approve requires one); tasks deny
   proceeds without one when it can't be derived — denying a task in an
   odd state can succeed where approving the same task fails.

## Verify

    c1i requests get "$TASK_ID"

Check taskView.task.state, not outcome, for whether anything is still
pending. outcome is omitted while it sits at *_OUTCOME_UNSPECIFIED; the
NDJSON views ("tasks list", "requests list") drop the key entirely in that
case rather than print the sentinel. Its absence there means "no result
yet" — but its presence does NOT mean "closed": a task can be
TASK_STATE_OPEN and already carry a real, non-UNSPECIFIED outcome (e.g. a
provisioning failure) while still waiting on a later step. state reaching
TASK_STATE_CLOSED is the only reliable end signal; use it, not the
presence of outcome, to decide whether a task is still pending.

If policy.next was non-empty in step 2, expect state to still read open
with a new policy.current after your action — the chain isn't done until
every step in it clears, not just the one you touched. Grant/revoke
provisioning can also lag briefly after approval, so poll rather than
checking once immediately.

## Common failures

- action not permitted on tasks approve/tasks deny (exit 1) — the
  authenticated identity isn't on the task's current policy step,
  confirmed ahead of time by actions in step 2 omitting
  TASK_ACTION_TYPE_APPROVE/_DENY.
- could not determine the current policy step for task ...; pass
  --policy-step-id explicitly, on tasks approve (exit 1) — no current step
  could be derived (approve requires one). tasks deny never fails this way
  on the same task; it just proceeds without a step. Supply
  --policy-step-id explicitly once you've read it via step 2's response.
- Entitlement-level grantPolicyId/revokePolicyId reading empty is normal,
  not a bug — it means the entitlement inherits the app's default policy.
  Read the task's embedded policy.policy, not the entitlement, to see
  what's actually governing a given task.
`

// docsGuides maps a guide name to its embedded content. Keep names stable —
// they're part of the CLI's public surface (an agent may hardcode
// "c1i docs guide register-mcp-server" in its own tooling).
var docsGuides = map[string]string{
	"register-mcp-server":               guideRegisterMCPServer,
	"assign-toolset-everyone":           guideAssignToolsetEveryone,
	"test-mcp-gateway":                  guideTestMCPGateway,
	"delegate-entitlement-provisioning": guideDelegateEntitlementProvisioning,
	"configure-new-app":                 guideConfigureNewApp,
	"request-access":                    guideRequestAccess,
	"inspect-and-approve-task":          guideInspectAndApproveTask,
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
