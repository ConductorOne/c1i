Run: `go run .` (from the repo root)

## Auth

```sh
# Browser-based login (OAuth device flow)
go run . auth login

# Direct credential login
go run . auth login --client-id=ID --client-secret=SECRET

# Verify stored credentials
go run . auth status
```

Credentials stored in macOS keychain under service `c1i/<tenant>`.

## Configuration

Tenant is required for all API commands. Set via (precedence order):
1. `--tenant` flag
2. `C1I_TENANT` env var
3. `~/.c1i.yaml` → `tenant: mycompany`

## Users

```sh
go run . users list [--query=NAME] [--email=EXACT] [--status=enabled|disabled|deleted] [--page-size=50] [--page-token=TOKEN]
```

NDJSON output: `{id, display_name, email, department, job_title, status}`

## Apps

```sh
go run . apps list [--page-size=50] [--page-token=TOKEN]
```

NDJSON output: `{id, display_name, description, user_count}`

## Accounts

```sh
go run . accounts list --app-id=ID [--status=enabled|disabled|deleted] [--type=user|service_account|system_account] [--unmapped-only] [--query=NAME] [--page-size=50] [--page-token=TOKEN]

go run . accounts set-owner --app-id=ID --app-user-id=AUID --user-id=UID
```

NDJSON output: `{id, app_id, display_name, email, username, identity_user_id, app_user_type, status}`

## Entitlements

```sh
go run . entitlements list [--app-id=ID] [--query=TEXT] [--page-size=50] [--page-token=TOKEN]
```

NDJSON output: `{id, app_id, display_name, description, slug, grant_count, purpose}`

## Tasks

```sh
go run . tasks list [--state=open|closed] [--query=TEXT] [--page-size=50] [--page-token=TOKEN]
```

NDJSON output: `{id, display_name, description, state, type, user_id, created_by_user_id, created_at, app_id, app_entitlement_id, outcome}`

## Raw API

```sh
# GET request
go run . api --path=/api/v1/apps

# POST request
go run . api --path=/api/v1/search/users --body='{"pageSize":10}'

# Auto-paginate all pages
go run . api --path=/api/v1/apps --paginate

# POST with pagination
go run . api --path=/api/v1/search/tasks --body='{"taskStates":["TASK_STATE_OPEN"]}' --paginate
```

Defaults to GET; auto-switches to POST when `--body` is set. Without `--paginate`, pretty-prints the full JSON response. With `--paginate`, unwraps the `list` array and outputs NDJSON (one item per line), same format as list commands.

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

## API usage patterns

- GET endpoints use `page_size` and `page_token` query params (snake_case)
- POST search endpoints use `pageSize` and `pageToken` in request body (camelCase)
- Response pagination: `nextPageToken` field (camelCase) in both cases
- `pageSize` max is 100
- Search response shape: `{"list": [...], "nextPageToken": "..."}`
- GET list response shape: `{"list": [...], "nextPageToken": "..."}`
- Search results nest objects: users under `"user"`, tasks under `"task"`, entitlements under `"appEntitlement"`, accounts under `"appUser"`
- GET list results are flat (apps list returns objects directly in `"list"`)

### Tasks

- Task states: `TASK_STATE_OPEN`, `TASK_STATE_CLOSED`
- Task types: `task.type.grant`, `task.type.revoke`, `task.type.certify`
- Task origins: `TASK_ORIGIN_AUTOMATION`, `TASK_ORIGIN_MANUAL`
- Task `.policy.history[]` contains the step-by-step execution log

### Campaigns / Access Reviews

The UI calls them "campaigns" (`/admin/campaigns/{id}`), the API calls them "access reviews". Campaign ID from a URL maps directly to the access review `id`.

## Notes

- All list command output is NDJSON; pipe to `jq` for filtering
- Auto-paginates unless `--page-token` is provided
- `--unmapped-only` filters client-side (accounts with no `identity_user_id`)
- Auth commands output human-readable text
- `api` command outputs pretty-printed JSON; with `--paginate` outputs NDJSON (one list item per line)
