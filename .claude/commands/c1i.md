Run: `go run .` (from the repo root)

This is the canonical command reference for working on c1i. For the agent-facing
copy that ships in the binary, see `cmd/skill.md` (`go run . docs skill`).

## Auth

```sh
# Browser-based login (OAuth device flow)
go run . auth login

# Direct credential login
go run . auth login --client-id=ID --client-secret=SECRET

# Verify stored credentials (and see which source served them)
go run . auth status

# Show the authenticated principal
go run . auth whoami

# Remove stored credentials
go run . auth logout
```

Credentials are read from the first source that has them, in order:
1. `C1I_CLIENT_ID` / `C1I_CLIENT_SECRET` env vars (read-only; both must be set)
2. OS keyring (Keychain / Credential Manager / Secret Service)
3. A `0600` JSON file under `os.UserConfigDir()` (headless Linux / CI / containers)

`auth login` writes to the keyring when available and falls back to the file
backend transparently. `auth status` reports which source is in use.

## Configuration

A C1 **URL** is required for all API commands. Set via (precedence order):
1. `--url` flag
2. `C1I_URL` env var
3. `~/.c1i.yaml` → `url: https://mycompany.conductor.one`

All equivalent: `--url https://mycompany.conductor.one`, `--url mycompany.conductor.one`,
`--url mycompany`.

## Users

```sh
go run . users list [--query=NAME] [--email=EXACT] [--status=enabled|disabled|deleted] [--page-size=50] [--page-token=TOKEN] [--limit=N]
```

NDJSON output: `{id, display_name, email, department, job_title, status}`

## Apps

```sh
go run . apps list [--page-size=50] [--page-token=TOKEN] [--limit=N]
```

NDJSON output: `{id, display_name, description, user_count}`

## Accounts

```sh
go run . accounts list --app-id=ID [--status=enabled|disabled|deleted] [--type=user|service_account|system_account] [--unmapped-only] [--query=NAME] [--page-size=50] [--page-token=TOKEN] [--limit=N]

go run . accounts set-owner --app-id=ID --app-user-id=AUID --user-id=UID
```

NDJSON output: `{id, app_id, display_name, email, username, identity_user_id, app_user_type, status}`

## Entitlements

```sh
go run . entitlements list [--app-id=ID] [--query=TEXT] [--page-size=50] [--page-token=TOKEN] [--limit=N]
```

NDJSON output: `{id, app_id, display_name, description, slug, grant_count, purpose}`

## Tasks

```sh
go run . tasks list [--state=open|closed] [--query=TEXT] [--assigned-to-me] [--page-size=50] [--page-token=TOKEN] [--limit=N]
go run . tasks approve --task-id=ID [--comment=TEXT]
go run . tasks deny --task-id=ID [--comment=TEXT]
go run . tasks comment --task-id=ID --comment=TEXT
```

NDJSON output: `{id, display_name, description, state, type, user_id, created_by_user_id, created_at, app_id, app_entitlement_id, outcome}`

## Connectors

```sh
go run . connectors list --app-id=ID [--page-size=50] [--page-token=TOKEN] [--limit=N]
```

NDJSON output: `{id, app_id, display_name, status}`

## Functions

```sh
go run . functions list [--published-only|--draft-only] [--page-size=50] [--page-token=TOKEN] [--limit=N]
go run . functions get <function-id>
go run . functions source <function-id> [--commit=CID] [--out-dir=PATH]
go run . functions commits <function-id> [--page-size=50] [--page-token=TOKEN] [--limit=N]
go run . functions usage <function-id>
```

`functions source` auto-resolves the function's published commit (falling back
to head) and base64-decodes the source files. Without `--out-dir`, each file is
printed to stdout with a `// ===== <name> =====` delimiter; with `--out-dir`,
files are written to disk (filenames are validated to stay inside the dir).
`functions usage` scans every automation and emits one row per step that calls
the function — useful before deleting a draft.

List NDJSON output: `{id, display_name, description, function_type, published_commit_id, head, is_draft, use_spn}`

## Automations

```sh
go run . automations list [--enabled-only] [--calls-function=FID] [--page-size=50] [--page-token=TOKEN] [--limit=N]
go run . automations get <automation-id>
go run . automations executions list [--state=done|error|pending|...] [--template-id=TID] [--page-size=50] [--page-token=TOKEN] [--limit=N]
```

Each `automations list` row carries `function_ids` (every distinct function the
automation invokes), so `--calls-function` answers "which automations call X?".
`executions list --state` accepts short forms (`done`, `error`, `pending`,
`creating`, `waiting`, `terminate`) or the full `AUTOMATION_EXECUTION_STATE_*`
enum. State and template filtering are client-side, so pair a narrow filter with
`--limit`.

List NDJSON output: `{id, display_name, description, enabled, last_executed_at, primary_trigger_type, is_draft, function_ids}`
Executions NDJSON output: `{id, automation_template_id, state, created_at, completed_at, duration, is_draft}`

## MCP

Admin surface for a registered MCP server. Tool/toolset commands take
`--app-id` and `--connector-id`.

```sh
# Tools
go run . mcp tools list    --app-id=ID --connector-id=CID [--page-size=50] [--page-token=TOKEN] [--limit=N]
go run . mcp tools get     --app-id=ID --connector-id=CID --id=TOOL_ID
go run . mcp tools search  --app-id=ID --connector-id=CID [--query=TEXT] [--state=...] [--classification=...] [--page-size=50] [--limit=N]
go run . mcp tools approve --app-id=ID --connector-id=CID --id=TOOL_ID [--state=approved|disabled|pending]
go run . mcp tools delete  --app-id=ID --connector-id=CID --id=TOOL_ID
go run . mcp tools history --app-id=ID --connector-id=CID --id=TOOL_ID [--page-size=50] [--limit=N]

# Toolsets (one AppEntitlement per toolset)
go run . mcp toolsets list                   --app-id=ID --connector-id=CID [--page-size=50] [--limit=N]
go run . mcp toolsets get                    --app-id=ID --connector-id=CID --id=TOOLSET_ID
go run . mcp toolsets create                 --app-id=ID --connector-id=CID --display-name=NAME [--description=TEXT]
go run . mcp toolsets update                 --app-id=ID --connector-id=CID --id=TOOLSET_ID [--display-name=NAME] [--description=TEXT]
go run . mcp toolsets delete                 --app-id=ID --connector-id=CID --id=TOOLSET_ID
go run . mcp toolsets get-by-entitlement     --app-id=ID --app-entitlement-id=AEID
go run . mcp toolsets requestable-connectors --user-id=UID

# Bindings (which tools belong to which toolset)
go run . mcp bindings list     --app-id=ID --connector-id=CID --toolset-id=TID [--page-size=50] [--limit=N]
go run . mcp bindings create   --app-id=ID --connector-id=CID --toolset-id=TID --tool-id=ID [--tool-id=...]
go run . mcp bindings delete   --app-id=ID --connector-id=CID --toolset-id=TID --tool-id=ID [--tool-id=...]
go run . mcp bindings by-tools --app-id=ID --connector-id=CID --tool-id=ID [--tool-id=...]
go run . mcp bindings history  --app-id=ID --connector-id=CID (--toolset-id=TID | --tool-id=ID) [--page-size=50] [--limit=N]
```

`mcp tools approve` is the standard post-registration step (PENDING_REVIEW →
APPROVED). Registering/deleting MCP servers themselves is not on this surface.
MCP list endpoints key results under the resource name (`tools`/`profiles`/
`bindings`), not the generic `list`; history endpoints return newest-first and
allow a page size up to 200. `mcp toolsets update` builds `updateMask`
(camelCase paths) from the flags that changed.

## Access Requests

```sh
go run . requests create grant --app-id=ID --entitlement-id=EID [--user-id=UID] [--description=TEXT] [--duration=DURATION] [--emergency]
go run . requests create revoke --app-id=ID --entitlement-id=EID [--user-id=UID] [--description=TEXT]
```

`--user-id` defaults to the authenticated user when omitted.

## Raw API

```sh
# GET request
go run . api --path=/api/v1/apps

# POST request
go run . api --path=/api/v1/search/users --body='{"pageSize":10}'

# Auto-paginate all pages
go run . api --path=/api/v1/apps --paginate

# Force the array field when auto-detect picks the wrong one
go run . api --path=/api/v1/automation_executions --paginate --list-key=automationExecutions
```

Defaults to GET; auto-switches to POST when `--body` is set. Without `--paginate`,
pretty-prints the full JSON response. With `--paginate`, drains each page's first
array-valued field (covers both `list` and typed keys like `automationExecutions`)
and outputs NDJSON, one item per line; use `--list-key=FIELD` to force a specific
field, and `--limit=N` to cap total output. If the server returns the same
`nextPageToken` twice in a row, the command aborts rather than looping forever.

## Documentation

```sh
# Search docs (no auth required)
go run . docs search "access reviews"

# Fetch a doc page
go run . docs page product/admin/campaigns

# List API endpoints (filterable)
go run . docs endpoints --filter=task

# Show full request/response schema for an endpoint
go run . docs endpoint /api/v1/search/tasks

# Dump raw OpenAPI spec
go run . docs openapi

# Print the embedded agent skill doc
go run . docs skill
```

## Common API endpoints

| Resource | Method | Path |
|---|---|---|
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
| List functions | GET | `/api/v1/functions` |

## API usage patterns

- GET endpoints use `page_size` and `page_token` query params (snake_case)
- POST search endpoints use `pageSize` and `pageToken` in request body (camelCase)
- Response pagination: `nextPageToken` field (camelCase) in both cases
- `pageSize` max is 100 (200 for MCP history endpoints)
- Search/GET-list response shape: `{"list": [...], "nextPageToken": "..."}`; some
  endpoints use a typed key instead (e.g. `automationExecutions`) — `api --paginate`
  drains the first array-valued field, overridable with `--list-key`
- Search results nest objects: users under `"user"`, tasks under `"task"`,
  entitlements under `"appEntitlement"`, accounts under `"appUser"`
- GET list results are flat (apps list returns objects directly in `"list"`)

### Tasks

- Task states: `TASK_STATE_OPEN`, `TASK_STATE_CLOSED`
- Task types: `task.type.grant`, `task.type.revoke`, `task.type.certify`
- Task origins: `TASK_ORIGIN_AUTOMATION`, `TASK_ORIGIN_MANUAL`
- Task `.policy.history[]` contains the step-by-step execution log

### Campaigns / Access Reviews

The UI calls them "campaigns" (`/admin/campaigns/{id}`), the API calls them
"access reviews". Campaign ID from a URL maps directly to the access review `id`.

## Notes

- All list command output is NDJSON; pipe to `jq` for filtering
- Auto-paginates unless `--page-token` is provided; `--limit=N` caps total output
- `--unmapped-only` filters client-side (accounts with no `identity_user_id`)
- Auth commands output human-readable text
- `api` outputs pretty-printed JSON; with `--paginate` outputs NDJSON (one item per line)
