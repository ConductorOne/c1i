---
name: c1i
description: CLI for the C1 (formerly ConductorOne) identity security platform — manage users, apps, entitlements, tasks, access reviews, and more.
version: {{VERSION}}
required_bins:
  - c1i
---

# c1i — C1 CLI

c1i is a command-line interface for the C1 API. It is designed for AI agents:
structured NDJSON output, auto-pagination, and built-in API discovery.

## Discovery First

**Before using any authenticated command, use `docs` subcommands to discover
endpoints and understand their schemas.** The `docs` commands require no
authentication and give you live, up-to-date API information.

```sh
# Search documentation by keyword
c1i docs search "access reviews"

# List all API endpoints (filterable)
c1i docs endpoints --filter=task

# Show full request/response schema for an endpoint
c1i docs endpoint /api/v1/search/tasks

# Fetch a full documentation page
c1i docs page product/admin/campaigns

# Dump the raw OpenAPI spec
c1i docs openapi
```

Always prefer `docs endpoints` and `docs endpoint` over the common endpoints
table below — the docs commands reflect the latest API surface.

## Auth

```sh
# Browser-based login (OAuth device flow)
c1i auth login

# Direct credential login
c1i auth login --client-id=ID --client-secret=SECRET

# Verify stored credentials (and see which source served them)
c1i auth status

# Identify the authenticated principal: user ID, roles, permissions, features
c1i auth whoami

# Remove stored credentials
c1i auth logout
```

Credentials are stored in the OS keyring when available, otherwise in a
0600 file under your config directory. For non-interactive / CI use, set
`C1I_CLIENT_ID` and `C1I_CLIENT_SECRET` (combined with `C1I_URL`) to skip
storage entirely. Run `c1i auth status` to see which source is in use.

## Configuration

The C1 URL is required for all API commands. Set via (precedence order):
1. `--url` flag (e.g. `--url=https://mycompany.conductor.one` or `--url=mycompany`)
2. `C1I_URL` env var
3. `~/.c1i.yaml` → `url: https://mycompany.conductor.one`

## Shell Completion

```sh
# bash
c1i completion bash > /etc/bash_completion.d/c1i  # or source it from ~/.bashrc

# zsh
c1i completion zsh > "${fpath[1]}/_c1i"

# fish
c1i completion fish > ~/.config/fish/completions/c1i.fish
```

## Version

```sh
c1i version    # or: c1i --version
```

## Commands

### Users

```sh
c1i users list [--query=NAME] [--email=EXACT] [--status=enabled|disabled|deleted] [--page-size=50] [--page-token=TOKEN] [--limit=N]
```

NDJSON fields: `id, display_name, email, department, job_title, status`

### Apps

```sh
c1i apps list [--page-size=50] [--page-token=TOKEN] [--limit=N]
```

NDJSON fields: `id, display_name, description, user_count`

### Accounts

```sh
c1i accounts list --app-id=ID [--status=enabled|disabled|deleted] [--type=user|service_account|system_account] [--unmapped-only] [--query=NAME] [--page-size=50] [--page-token=TOKEN] [--limit=N]

c1i accounts set-owner --app-id=ID --app-user-id=AUID --user-id=UID
```

NDJSON fields: `id, app_id, display_name, email, username, identity_user_id, app_user_type, status`

### Entitlements

```sh
c1i entitlements list [--app-id=ID] [--query=TEXT] [--page-size=50] [--page-token=TOKEN] [--limit=N]
```

NDJSON fields: `id, app_id, display_name, description, slug, grant_count, purpose`

### Tasks

```sh
c1i tasks list [--state=open|closed] [--query=TEXT] [--assigned-to-me] [--page-size=50] [--page-token=TOKEN] [--limit=N]
```

NDJSON fields: `id, display_name, description, state, type, user_id, created_by_user_id, created_at, app_id, app_entitlement_id, outcome`

`outcome` is omitted on open tasks (the underlying enum default
`*_OUTCOME_UNSPECIFIED` is suppressed). It is present on closed tasks with
values like `GRANT_OUTCOME_APPROVED`, `GRANT_OUTCOME_DENIED`, etc.

### Connectors

```sh
c1i connectors list --app-id=ID [--page-size=50] [--page-token=TOKEN] [--limit=N]
```

NDJSON fields: `id, app_id, display_name, status`

`--app-id` is required.

### MCP (tools, toolsets, bindings)

```sh
# --- Tools (discovered from a registered MCP server) ---
c1i mcp tools list    --app-id=ID --connector-id=CID [--page-size=50] [--page-token=TOKEN] [--limit=N]
c1i mcp tools get     --app-id=ID --connector-id=CID --id=TOOL_ID
c1i mcp tools search  --app-id=ID --connector-id=CID [--query=TEXT] [--state=...]... [--classification=...]... [--page-size=50] [--page-token=TOKEN] [--limit=N]
c1i mcp tools approve --app-id=ID --connector-id=CID --id=TOOL_ID [--state=approved|disabled|pending]
c1i mcp tools delete  --app-id=ID --connector-id=CID --id=TOOL_ID
c1i mcp tools history --app-id=ID --connector-id=CID --id=TOOL_ID [--page-size=50] [--page-token=TOKEN] [--limit=N]

# --- Toolsets (access profiles — one AppEntitlement per toolset) ---
c1i mcp toolsets list                   --app-id=ID --connector-id=CID [--page-size=50] [--page-token=TOKEN] [--limit=N]
c1i mcp toolsets get                    --app-id=ID --connector-id=CID --id=TOOLSET_ID
c1i mcp toolsets create                 --app-id=ID --connector-id=CID --display-name=NAME [--description=TEXT]
c1i mcp toolsets update                 --app-id=ID --connector-id=CID --id=TOOLSET_ID [--display-name=NAME] [--description=TEXT]
c1i mcp toolsets delete                 --app-id=ID --connector-id=CID --id=TOOLSET_ID
c1i mcp toolsets get-by-entitlement     --app-id=ID --app-entitlement-id=AEID
c1i mcp toolsets requestable-connectors --user-id=UID

# --- Bindings (which tools are inside which toolsets) ---
c1i mcp bindings list     --app-id=ID --connector-id=CID --toolset-id=TID [--page-size=50] [--page-token=TOKEN] [--limit=N]
c1i mcp bindings create   --app-id=ID --connector-id=CID --toolset-id=TID --tool-id=ID [--tool-id=...]
c1i mcp bindings delete   --app-id=ID --connector-id=CID --toolset-id=TID --tool-id=ID [--tool-id=...]
c1i mcp bindings by-tools --app-id=ID --connector-id=CID --tool-id=ID [--tool-id=...]
c1i mcp bindings history  --app-id=ID --connector-id=CID (--toolset-id=TID | --tool-id=ID) [--page-size=50] [--page-token=TOKEN] [--limit=N]
```

`mcp tools list`/`search` NDJSON fields: `id, app_id, connector_id, tool_name, app_entitlement_id, display_name, description, classification, state, visibility`

`mcp tools history` emits one raw history entry per line, **newest-first** — each carries the full tool snapshot at that version plus the change-history metadata envelope (actor, change_kind, created_at, trace_id, syslog_event_id, annotations).

`mcp toolsets list` NDJSON fields: `id, app_id, connector_id, display_name, description, app_entitlement_id, tool_count`

`mcp toolsets requestable-connectors` NDJSON fields: `app_id, connector_id` (one per connector with at least one toolset the user can request — not paginated server-side).

`mcp bindings list` NDJSON fields: `app_id, connector_id, access_profile_id, mcp_tool_id, created_at`

`mcp bindings by-tools` NDJSON shape: `{"mcp_tool_id": "...", "toolsets": [...]}` per requested tool. Tools with no bindings still appear with `"toolsets": []` so callers can distinguish "no bindings" from "not requested".

`mcp bindings history` requires exactly one of `--toolset-id` (history of all tools in a toolset) or `--tool-id` (history of all toolsets containing a tool). One raw entry per line, with a list-history metadata envelope plus the items added or removed in that transaction.

Tool states: `MCP_TOOL_STATE_PENDING_REVIEW`, `MCP_TOOL_STATE_APPROVED`, `MCP_TOOL_STATE_DISABLED`, `MCP_TOOL_STATE_REMOVED`.
Tool classifications: `TOOL_CLASSIFICATION_READ`, `_WRITE`, `_DESTRUCTIVE`, `_SENSITIVE`, `_DANGEROUS`.

Approving tools is the standard post-registration step: a newly registered MCP
server discovers its tools in `PENDING_REVIEW`, then an admin moves each to
`APPROVED` for the gateway to proxy calls. Note: registering / deleting MCP
servers themselves is not in the public REST surface — drive that via the
`mcp-setup` skill or the support dashboard.

### Functions

```sh
c1i functions list [--published-only|--draft-only] [--page-size=50] [--page-token=TOKEN] [--limit=N]
c1i functions get <function-id>
c1i functions source <function-id> [--commit=CID] [--out-dir=PATH]
c1i functions commits <function-id> [--page-size=50] [--page-token=TOKEN] [--limit=N]
c1i functions usage <function-id>
```

`functions source` auto-resolves the function's published commit (falling
back to head) and base64-decodes the source files. Without `--out-dir` each
file is printed to stdout with `// ===== <name> =====` delimiter headers.

`functions usage` scans all automations and emits one NDJSON row per step
that calls the given function ID — useful before deleting a draft to see if
anything still depends on it.

List NDJSON fields: `id, display_name, description, function_type, published_commit_id, head, is_draft, use_spn`

### Automations

```sh
c1i automations list [--enabled-only] [--calls-function=FID] [--page-size=50] [--page-token=TOKEN] [--limit=N]
c1i automations get <automation-id>
c1i automations executions list [--state=done|error|pending|...] [--template-id=TID] [--page-size=50] [--page-token=TOKEN] [--limit=N]
```

`--calls-function` filters to automations that invoke the given function ID
in any of their steps — useful before deleting a function, or to find an
example automation that exercises one.

`executions list --state` accepts the short forms (`done`, `error`,
`pending`, `creating`, `waiting`, `terminate`) or the full
`AUTOMATION_EXECUTION_STATE_*` enum. Filtering is applied client-side:
the endpoint doesn't yet support server-side state filters, so a narrow
filter still scans every page returned — combine with `--limit` to bound
the work.

List NDJSON fields: `id, display_name, description, enabled, last_executed_at, primary_trigger_type, is_draft, function_ids`

Executions NDJSON fields: `id, automation_template_id, state, created_at, completed_at, duration, is_draft`

### Access Requests

```sh
c1i requests create grant --app-id=ID --entitlement-id=EID [--user-id=UID] [--description=TEXT] [--duration=DURATION] [--emergency]
c1i requests create revoke --app-id=ID --entitlement-id=EID [--user-id=UID] [--description=TEXT]
```

`--user-id` defaults to the authenticated user when omitted. `--description` is
free-form justification text shown to approvers.

### Task Actions

```sh
c1i tasks approve --task-id=ID [--comment=TEXT]
c1i tasks deny --task-id=ID [--comment=TEXT]
c1i tasks comment --task-id=ID --comment=TEXT
```

### Raw API

```sh
# GET request
c1i api --path=/api/v1/apps

# POST request
c1i api --path=/api/v1/search/users --body='{"pageSize":10}'

# Auto-paginate all pages
c1i api --path=/api/v1/apps --paginate

# POST with pagination
c1i api --path=/api/v1/search/tasks --body='{"taskStates":["TASK_STATE_OPEN"]}' --paginate
```

Defaults to GET; auto-switches to POST when `--body` is set. Without
`--paginate`, pretty-prints the full JSON response. With `--paginate`, unwraps
the first array-valued field in the response and outputs NDJSON (one item per
line) — works for endpoints that wrap items under `list` (most) as well as
typed keys (`automationExecutions`, `automations`, etc.). Pass
`--list-key=<field>` to force a specific field when the auto-detect picks the
wrong one (rare).

If you GET an endpoint that requires POST (e.g. `/api/v1/search/*`), the
server returns 404 or 405 and `c1i api` will print a one-line hint
suggesting `--body` or `--method=POST`.

If the server returns the same `nextPageToken` twice in a row (some endpoints
silently ignore the cursor), `c1i api --paginate` aborts with a clear error
instead of looping forever. Drop `--paginate` and use a single call if you
hit this.

## Common API Endpoints

> Use `c1i docs endpoints` for the latest list. Note: a few endpoints
> (notably `/api/v1/access_review*`) exist on the server but are not in
> the public OpenAPI spec, so they only appear in this table.

| Resource | Method | Path |
|---|---|---|
| Current principal (whoami) | GET | `/api/v1/auth/introspect` |
| Get user | GET | `/api/v1/users/{id}` |
| Search users | POST | `/api/v1/search/users` |
| Get app | GET | `/api/v1/apps/{id}` |
| List apps | GET | `/api/v1/apps` |
| Get entitlement | GET | `/api/v1/apps/{app_id}/entitlements/{id}` |
| Search entitlements | POST | `/api/v1/search/entitlements` |
| Search app accounts | POST | `/api/v1/search/app_users` |
| Get task | GET | `/api/v1/tasks/{id}` |
| Search tasks | POST | `/api/v1/search/tasks` |
| Get access review | GET | `/api/v1/access_review/{id}` |
| List access reviews | GET | `/api/v1/access_reviews` |
| List automations | GET | `/api/v1/automations` |
| Get automation | GET | `/api/v1/automations/{id}` |
| List automation executions | GET | `/api/v1/automation_executions` |
| List MCP tools | GET | `/api/v1/apps/{app_id}/connectors/{connector_id}/mcp_tools` |
| Get MCP tool | GET | `/api/v1/apps/{app_id}/connectors/{connector_id}/mcp_tools/{id}` |
| Search MCP tools | POST | `/api/v1/apps/{app_id}/connectors/{connector_id}/mcp_tools/search` |
| Update MCP tool (approve) | POST | `/api/v1/apps/{app_id}/connectors/{connector_id}/mcp_tools/{id}` |
| Delete MCP tool | DELETE | `/api/v1/apps/{app_id}/connectors/{connector_id}/mcp_tools/{id}` |
| MCP tool change history | GET | `/api/v1/apps/{app_id}/connectors/{connector_id}/mcp_tools/{id}/history` |
| List MCP toolsets | GET | `/api/v1/apps/{app_id}/connectors/{connector_id}/mcp_toolsets` |
| Get MCP toolset | GET | `/api/v1/apps/{app_id}/connectors/{connector_id}/mcp_toolsets/{id}` |
| Create MCP toolset | POST | `/api/v1/apps/{app_id}/connectors/{connector_id}/mcp_toolsets` |
| Update MCP toolset | POST | `/api/v1/apps/{app_id}/connectors/{connector_id}/mcp_toolsets/{id}` |
| Delete MCP toolset | DELETE | `/api/v1/apps/{app_id}/connectors/{connector_id}/mcp_toolsets/{id}` |
| Resolve toolset by entitlement | GET | `/api/v1/apps/{app_id}/mcp_toolsets/by_app_entitlement_id/{app_entitlement_id}` |
| User's requestable toolset connectors | GET | `/api/v1/users/{user_id}/mcp_toolsets/requestable_connectors` |
| List MCP toolset bindings | GET | `/api/v1/apps/{app_id}/connectors/{connector_id}/mcp_toolsets/{toolset_id}/tool_bindings` |
| Create MCP toolset bindings | POST | `/api/v1/apps/{app_id}/connectors/{connector_id}/mcp_toolsets/{toolset_id}/tool_bindings` |
| Delete MCP toolset bindings | POST | `/api/v1/apps/{app_id}/connectors/{connector_id}/mcp_toolsets/{toolset_id}/tool_bindings/delete` |
| MCP bindings by tools (reverse) | POST | `/api/v1/apps/{app_id}/connectors/{connector_id}/tool_bindings/by_tools` |
| MCP toolset binding history | GET | `/api/v1/apps/{app_id}/connectors/{connector_id}/mcp_toolsets/{toolset_id}/tool_bindings/history` |
| MCP tool binding history (reverse) | GET | `/api/v1/apps/{app_id}/connectors/{connector_id}/tool_bindings/by_tool/{tool_id}/history` |

## API Usage Patterns

- GET endpoints use `page_size` and `page_token` query params (snake_case).
- POST search endpoints use `pageSize` and `pageToken` in the request body (camelCase).
- Response pagination: `nextPageToken` field (camelCase) in both cases.
- `pageSize` max is 100.
- Search response shape: `{"list": [...], "nextPageToken": "..."}`
- GET list response shape: `{"list": [...], "nextPageToken": "..."}`
- Search results nest objects: users under `"user"`, tasks under `"task"`,
  entitlements under `"appEntitlement"`, accounts under `"appUser"`.
- GET list results are flat (apps list returns objects directly in `"list"`).
- **MCP list endpoints break this convention**: the wrapper key matches the
  resource — `{"tools": [...]}` for `mcp_tools`, `{"profiles": [...]}` for
  `mcp_toolsets`, `{"bindings": [...]}` for `tool_bindings`. The
  `nextPageToken` field is still present. History endpoints (`/history`)
  and the reverse-binding endpoint (`/tool_bindings/by_tools`) keep the
  generic `"list"` shape.

### Tasks

- Task states: `TASK_STATE_OPEN`, `TASK_STATE_CLOSED`
- Task types: `task.type.grant`, `task.type.revoke`, `task.type.certify`
- Task origins: `TASK_ORIGIN_AUTOMATION`, `TASK_ORIGIN_MANUAL`
- Task `.policy.history[]` contains the step-by-step execution log.

### Campaigns / Access Reviews

The UI calls them "campaigns" (`/admin/campaigns/{id}`), the API calls them
"access reviews". A campaign ID from a URL maps directly to the access review `id`.

## Output Formats

- **List/search commands**: NDJSON (one JSON object per line). Pipe to `jq` for filtering.
- **Single-object commands**: Pretty-printed JSON.
- **Auth commands**: Human-readable plain text.
- **`api` command**: Pretty-printed JSON by default; NDJSON with `--paginate`.
- All list commands auto-paginate. Passing `--page-token` disables auto-pagination.
- `--page-size` controls the per-call batch size (max 100). Use `--limit N` to cap the *total* number of results emitted; auto-pagination stops fetching new pages once the cap is reached.
- `--unmapped-only` (accounts) filters client-side: only accounts with no `identity_user_id`.

## Discovering a New Endpoint

When you need to call an API endpoint you haven't used before:

1. **Search for it**: `c1i docs endpoints --filter=<keyword>` — matches
   path, summary, operation ID, and description (so functional words like
   "current user" or "self approval" work even when the path is opaque).
2. **Inspect the schema**: `c1i docs endpoint <path>` to see request body fields and response shape.
3. **Try it**: `c1i api --path=<path>` (GET) or `c1i api --path=<path> --body='...'` (POST).
4. **Paginate if needed**: Add `--paginate` to unwrap the response's first array-valued field (e.g. `list`, `automationExecutions`) into NDJSON. Use `--list-key=<field>` to force a specific field.
5. **Read the docs**: `c1i docs search <topic>` and `c1i docs page <path>` for context beyond the API reference.
