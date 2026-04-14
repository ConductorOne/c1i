---
name: c1i
description: CLI for the C1 identity security platform — manage users, apps, entitlements, tasks, and access reviews.
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

# Verify stored credentials
c1i auth status
```

Credentials are stored in the OS keychain under service `c1i/<tenant>`.

## Configuration

Tenant is required for all API commands. Set via (precedence order):
1. `--tenant` flag
2. `C1I_TENANT` env var
3. `~/.c1i.yaml` → `tenant: mycompany`

## Commands

### Users

```sh
c1i users list [--query=NAME] [--email=EXACT] [--status=enabled|disabled|deleted] [--page-size=50] [--page-token=TOKEN]
```

NDJSON fields: `id, display_name, email, department, job_title, status`

### Apps

```sh
c1i apps list [--page-size=50] [--page-token=TOKEN]
```

NDJSON fields: `id, display_name, description, user_count`

### Accounts

```sh
c1i accounts list --app-id=ID [--status=enabled|disabled|deleted] [--type=user|service_account|system_account] [--unmapped-only] [--query=NAME] [--page-size=50] [--page-token=TOKEN]

c1i accounts set-owner --app-id=ID --app-user-id=AUID --user-id=UID
```

NDJSON fields: `id, app_id, display_name, email, username, identity_user_id, app_user_type, status`

### Entitlements

```sh
c1i entitlements list [--app-id=ID] [--query=TEXT] [--page-size=50] [--page-token=TOKEN]
```

NDJSON fields: `id, app_id, display_name, description, slug, grant_count, purpose`

### Tasks

```sh
c1i tasks list [--state=open|closed] [--query=TEXT] [--page-size=50] [--page-token=TOKEN]
```

NDJSON fields: `id, display_name, description, state, type, user_id, created_by_user_id, created_at, app_id, app_entitlement_id, outcome`

### Connectors

```sh
c1i connectors list [--page-size=50] [--page-token=TOKEN]
```

### Access Requests

```sh
c1i requests create grant --app-id=ID --entitlement-id=EID --user-id=UID [--justification=TEXT] [--duration=DURATION]
c1i requests create revoke --app-id=ID --entitlement-id=EID --user-id=UID [--justification=TEXT]
```

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

## Common API Endpoints

> Use `c1i docs endpoints` for the latest list — this table is a quick reference.

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
- `--unmapped-only` (accounts) filters client-side: only accounts with no `identity_user_id`.

## Discovering a New Endpoint

When you need to call an API endpoint you haven't used before:

1. **Search for it**: `c1i docs endpoints --filter=<keyword>`
2. **Inspect the schema**: `c1i docs endpoint <path>` to see request body fields and response shape.
3. **Try it**: `c1i api --path=<path>` (GET) or `c1i api --path=<path> --body='...'` (POST).
4. **Paginate if needed**: Add `--paginate` to unwrap `list` arrays into NDJSON.
5. **Read the docs**: `c1i docs search <topic>` and `c1i docs page <path>` for context beyond the API reference.
