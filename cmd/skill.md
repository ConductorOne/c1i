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
the `list` array and outputs NDJSON (one item per line).

If you GET an endpoint that requires POST (e.g. `/api/v1/search/*`), the
server returns 404 or 405 and `c1i api` will print a one-line hint
suggesting `--body` or `--method=POST`.

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
4. **Paginate if needed**: Add `--paginate` to unwrap `list` arrays into NDJSON.
5. **Read the docs**: `c1i docs search <topic>` and `c1i docs page <path>` for context beyond the API reference.
