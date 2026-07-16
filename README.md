# c1i

**C1 Interface** — an agent-oriented CLI for C1, and it looks like `cli`. Get it?

> **Beta** — stabilizing toward 1.0. The core commands, flags, and output formats are stable; smaller changes may still land before 1.0.

A command-line interface for the [C1](https://www.conductorone.com) API designed for AI agents.
Structured output (NDJSON/JSON), built-in API docs, and auto-pagination.
For a human-friendly CLI, see [cone](https://github.com/ConductorOne/cone).

## Quick Start

```sh
# Install
go install github.com/ConductorOne/c1i@latest

# Log in (opens browser)
c1i auth login --url mycompany.conductor.one

# List users
c1i users list

# Explore the API — no credentials needed
c1i docs search "access reviews"
c1i docs endpoints --filter task
```

## Commands

### Users

```sh
c1i users list [--query <text>] [--email <email>] [--status enabled|disabled|deleted] [--page-size N] [--page-token TOKEN] [--limit N]
c1i users get <user-id>
```

### Apps

```sh
c1i apps list [--page-size N] [--page-token TOKEN] [--limit N]
c1i apps get <app-id>
```

### Accounts

```sh
c1i accounts list --app-id <id> [--status enabled|disabled|deleted] [--type user|service_account|system_account] [--unmapped-only] [--query <text>] [--page-size N] [--page-token TOKEN] [--limit N]

c1i accounts set-owner --app-id <id> --app-user-id <id> --user-id <id>
```

### Entitlements

```sh
c1i entitlements list [--app-id <id>] [--query <text>] [--page-size N] [--page-token TOKEN] [--limit N]
c1i entitlements get <entitlement-id> --app-id <id>
```

### Grants ("who has access")

```sh
c1i grants list --app-id <id> --entitlement-id <id>   # who holds an entitlement
c1i grants list --user-id <id>                         # what a C1 identity has, across apps
c1i grants list --app-user-id <id>                     # what an app account holds
c1i grants list --app-id <id>                          # every grant in an app
```

Grants are the bindings between accounts/users and entitlements. At least one
filter is required. Each NDJSON row includes the entitlement, the account
(`app_user_*`) and its `identity_user_id`, timestamps (`created_at`,
`deprovision_at`), and `grant_source_count` — `0` for a direct grant, or the
number of groups/roles the access is inherited through.

### Tasks

```sh
c1i tasks list [--state open|closed] [--query <text>] [--assigned-to-me] [--page-size N] [--page-token TOKEN] [--limit N]
c1i tasks approve --task-id <id> [--policy-step-id <id>] [--comment <text>]
c1i tasks deny --task-id <id> [--policy-step-id <id>] [--comment <text>]
c1i tasks comment --task-id <id> --comment <text>
```

`approve`/`deny` target a specific policy step. If `--policy-step-id` is
omitted, the task's currently executing step is fetched and used automatically.

### Connectors

```sh
c1i connectors list --app-id <id> [--page-size N] [--page-token TOKEN] [--limit N]
```

### Functions

```sh
c1i functions list [--published-only | --draft-only] [--page-size N] [--page-token TOKEN] [--limit N]
c1i functions get <function-id>
c1i functions source <function-id> [--commit <id>] [--out-dir <path>]
c1i functions commits <function-id> [--page-size N] [--page-token TOKEN] [--limit N]
c1i functions usage <function-id>
```

`functions source` auto-resolves the function's published commit (falling back to its head/latest draft) and base64-decodes the source files. Without `--out-dir`, each file is printed to stdout with a `// ===== <name> =====` delimiter; with `--out-dir`, files are written to disk. `functions usage` scans every automation and emits one row per step that calls the given function — useful before deleting a draft.

### Automations

```sh
c1i automations list [--enabled-only] [--calls-function <fid>] [--page-size N] [--page-token TOKEN] [--limit N]
c1i automations get <automation-id>
c1i automations executions list [--state done|error|pending|...] [--template-id <tid>] [--page-size N] [--page-token TOKEN] [--limit N]
```

Each `automations list` row includes `function_ids` (every distinct function the automation invokes), so `--calls-function` can answer "which automations call function X?". `executions list --state` accepts the short forms (`done`, `error`, `pending`, ...) or the full `AUTOMATION_EXECUTION_STATE_*` enum; state and template filtering are applied client-side, so pair a narrow filter with `--limit` to bound the work.

### MCP

Drive the MCP admin surface (tools, toolsets, and bindings) for a registered MCP server. Tool and toolset commands take `--app-id` and `--connector-id`.

```sh
# Tools
c1i mcp tools list    --app-id <id> --connector-id <id> [--page-size N] [--page-token TOKEN] [--limit N]
c1i mcp tools get     --app-id <id> --connector-id <id> --id <tool-id>
c1i mcp tools search  --app-id <id> --connector-id <id> [--query <text>] [--state ...] [--classification ...] [--page-size N] [--limit N]
c1i mcp tools approve --app-id <id> --connector-id <id> --id <tool-id> [--state approved|disabled|pending]
c1i mcp tools delete  --app-id <id> --connector-id <id> --id <tool-id>
c1i mcp tools history --app-id <id> --connector-id <id> --id <tool-id> [--page-size N] [--limit N]

# Toolsets (admin-curated tool groupings; one AppEntitlement per toolset)
c1i mcp toolsets list                   --app-id <id> --connector-id <id> [--page-size N] [--limit N]
c1i mcp toolsets get                    --app-id <id> --connector-id <id> --id <toolset-id>
c1i mcp toolsets create                 --app-id <id> --connector-id <id> --display-name <name> [--description <text>]
c1i mcp toolsets update                 --app-id <id> --connector-id <id> --id <toolset-id> [--display-name <name>] [--description <text>]
c1i mcp toolsets delete                 --app-id <id> --connector-id <id> --id <toolset-id>
c1i mcp toolsets get-by-entitlement     --app-id <id> --app-entitlement-id <aeid>
c1i mcp toolsets requestable-connectors --user-id <uid>

# Bindings (which tools belong to which toolset)
c1i mcp bindings list     --app-id <id> --connector-id <id> --toolset-id <tid> [--page-size N] [--limit N]
c1i mcp bindings create   --app-id <id> --connector-id <id> --toolset-id <tid> --tool-id <id> [--tool-id <id> ...]
c1i mcp bindings delete   --app-id <id> --connector-id <id> --toolset-id <tid> --tool-id <id> [--tool-id <id> ...]
c1i mcp bindings by-tools --app-id <id> --connector-id <id> --tool-id <id> [--tool-id <id> ...]
c1i mcp bindings history  --app-id <id> --connector-id <id> (--toolset-id <tid> | --tool-id <id>) [--page-size N] [--limit N]
```

`mcp tools approve` is the standard post-registration step: newly discovered tools start in `PENDING_REVIEW`, and an admin approves each one for the gateway to proxy calls. Registering or deleting MCP servers themselves is not part of this surface. History endpoints return records newest-first.

### Access Requests

```sh
c1i requests create grant --app-id <id> --entitlement-id <eid> [--user-id <uid>] [--description <text>] [--duration <duration>] [--emergency]
c1i requests create revoke --app-id <id> --entitlement-id <eid> [--user-id <uid>] [--description <text>]
c1i requests list [--user-id <id> | --all] [--app-id <id>] [--entitlement-id <id>] [--state open|closed] [--type grant|revoke] [--page-size N] [--page-token TOKEN] [--limit N]
c1i requests get <request-id>
```

On `create`, `--user-id` defaults to the authenticated user when omitted.

`requests list` is the requester lens on access requests (the grant/revoke tasks
you file): by default it shows requests you opened or are the subject of —
complementing `tasks list`, which is the approver's My Work lens. Use `--user-id`
to scope to another user or `--all` for every request in the tenant. `requests
get` fetches a single request (the `task_id` returned by `requests create`) as
pretty JSON, including its current policy step and outcome.

### Export

```sh
c1i export events [--since <rfc3339>] [--until <rfc3339>] [--since-event-uid <uid>] [--sort asc|desc] [--page-size N] [--page-token TOKEN] [--limit N]
```

`export events` dumps the C1 system log — OCSF-formatted audit events — as an
NDJSON stream (one event per line), auto-paginating through the whole result.
Redirect it to a file to archive events or ship them to an external system:

```sh
c1i export events > audit.ndjson                                  # everything, oldest first
c1i export events --since 2026-07-01T00:00:00Z --until 2026-07-08T00:00:00Z
c1i export events --since-event-uid <last-uid>                    # incremental sync
```

`--sort` defaults to `asc` (chronological), which pairs with `--since-event-uid`
for incremental sync. `--fields` works on events too (e.g. `--fields
activity_name,actor.user.email_addr,time`).

### Raw API

```sh
# GET request
c1i api --path /api/v1/apps

# POST request
c1i api --path /api/v1/search/users --body '{"pageSize":10}'

# Other methods — --method takes GET, POST, PUT, PATCH, or DELETE
c1i api --path /api/v1/apps/<app>/connectors/<conn>/mcp_tools/<id> --method DELETE

# Read the body from a file, or stdin with "-"
c1i api --path /api/v1/search/users --body-file query.json
echo '{"pageSize":10}' | c1i api --path /api/v1/search/users --body-file -

# Add query params and headers (both repeatable)
c1i api --path /api/v1/apps --query page_size=5 --header X-Request-Id=abc123

# Auto-paginate through all results (NDJSON output, one item per line)
c1i api --path /api/v1/apps --paginate

# Force the array field to drain when auto-detection picks the wrong one
c1i api --path /api/v1/automation_executions --paginate --list-key automationExecutions
```

The method defaults to GET, or POST when a body is set; pass `--method` for
PUT/PATCH/DELETE. The body comes from `--body` (inline JSON) or `--body-file` (a
file, or `-` for stdin) — the two are mutually exclusive. `--query key=value` and
`--header key=value` are both repeatable. When `--paginate` is used, each page's first array-valued field is unwrapped and each item is emitted as a single line of NDJSON — the same format used by list commands. This covers both the canonical `list` key and typed keys like `automationExecutions`; use `--list-key <field>` to force a specific field. If the server returns the same `nextPageToken` twice in a row, `c1i` aborts with an error rather than looping forever. Without `--paginate`, the full JSON response is pretty-printed.

### API Discovery & Documentation

The `docs` commands require no C1 credentials — agents can use them to explore the API before authenticating.

```sh
# Search documentation
c1i docs search "access reviews"

# Fetch a documentation page
c1i docs page product/admin/campaigns

# List API endpoints (filtered)
c1i docs endpoints --filter task

# Show full request/response schema for an endpoint
c1i docs endpoint /api/v1/search/tasks

# Dump the raw OpenAPI spec
c1i docs openapi

# Print the agent skill/reference doc (write to a file with --output)
c1i docs skill [--output SKILL.md]
```

## Output Conventions

- **List commands** (`users list`, `apps list`, etc.) output NDJSON (one JSON object per line).
- **`api`** outputs pretty-printed JSON. With `--paginate`, outputs NDJSON (one list item per line).
- **`docs`** commands output NDJSON (`search`, `endpoints`), pretty JSON (`endpoint`, `openapi` is YAML), or plain text (`page`).
- List commands auto-paginate by default. Pass `--page-token` to fetch a single page manually.
- `--page-size` controls the per-call batch size (max 100). Use `--limit N` to cap the *total* number of results emitted; auto-pagination stops fetching new pages once the cap is reached.

### Field selection

`--fields` trims every emitted JSON object to just the keys you name — a big
token saver when an agent only needs a couple of fields from a large list.

```sh
# Only id and email from each user
c1i users list --fields id,email

# Dot-paths select nested fields; nesting is preserved in the output
c1i api --path /api/v1/apps --paginate --fields id,displayName
c1i functions get <id> --fields id,displayName,publishedCommitId
```

- Comma-separated; use dot-paths (`user.email`) for nested access.
- Matches the keys **as they appear in the command's output** (e.g. list rows
  use `id`, `display_name`; raw `api` output uses the API's own camelCase keys).
- Missing fields are silently omitted, so requesting a superset is safe.
- Also settable via `C1I_FIELDS`. Applies to read output — list commands, `api`,
  and single-object `get` commands. Mutation confirmations (create/update/delete)
  are never projected, so a session-wide `C1I_FIELDS` can't hide their status.

### Errors & exit codes

On failure, `c1i` writes an error to stderr and exits with a code an agent can
branch on without parsing text:

| Code | Meaning |
|------|---------|
| `0` | success |
| `1` | generic / unclassified error |
| `2` | usage error (bad flags or arguments) |
| `3` | not authenticated, or API returned `401`/`403` |
| `4` | API returned `404` (not found) |
| `5` | API returned `429` (rate limited — back off and retry) |
| `6` | API returned `5xx` (server error) |

Pass `--error-format json` (or `C1I_ERROR_FORMAT=json`) to get a machine-readable
error object instead of the default `Error: <msg>` line. For API errors it
includes the status, method, path, and response body:

```sh
$ c1i api --path /api/v1/nope --error-format json
{"error":"API error: API GET /api/v1/nope returned 404: ...","status":404,"method":"GET","path":"/api/v1/nope","body":{"message":"not found"}}
```

The `body` is embedded as JSON when the API returned JSON, otherwise as a string.

## Configuration

c1i requires a C1 **URL**. You can pass a full URL, a raw domain, or a legacy short tenant name. Set it via (in order of precedence):

1. `--url` flag
2. `C1I_URL` environment variable
3. `~/.c1i.yaml` config file:
   ```yaml
   url: https://mycompany.conductor.one
   ```

All of these are equivalent:
- `--url https://mycompany.conductor.one`
- `--url mycompany.conductor.one`
- `--url mycompany`

For credential storage, see [Credential sources](#credential-sources) below.

### Retries

Transient API failures are retried automatically with exponential backoff and
jitter, honoring a `Retry-After` header when the server sends one. This keeps
long auto-paginated pulls from failing on a single rate-limit blip. What gets
retried depends on the request, to avoid duplicating side effects:

- **`429 Too Many Requests`** — retried for every command (the request is
  rejected before the server processes it, so a retry is always safe).
- **Transient `5xx` (500, 502, 503, 504) and network errors** — retried only for
  idempotent reads and updates (GET/PUT/DELETE). Non-idempotent `POST` mutations
  (e.g. `requests create`, `tasks approve`) are **not** retried on these, since
  the server may have already applied the change before the failure.
  Non-transient 5xx (501 Not Implemented, 505, 511) are never retried.

Control the retry budget (attempts *after* the first try) via, in order of
precedence:

1. `--max-retries N` flag (applies to any command)
2. `C1I_MAX_RETRIES` environment variable
3. Default: `4`

Set `--max-retries 0` to disable retries entirely. Non-retryable responses
(4xx other than 429, and 501/505) fail immediately.

### Dry run

`--dry-run` (or `C1I_DRY_RUN=1`) previews a mutating request — its method, path,
and pretty-printed JSON body — and returns without sending it:

```sh
$ c1i requests create grant --app-id A1 --entitlement-id E1 --user-id U1 --dry-run
[dry-run] POST /api/v1/task/grant
{
  "appEntitlementId": "E1",
  "appId": "A1",
  "identityUserId": "U1"
}
```

It applies to every write command (`requests create`, `tasks approve/deny/comment`,
`accounts set-owner`, the `mcp` mutations) and to non-GET `api` calls, and never
sends the mutation itself. Most previews run fully offline — no credentials
required. The exception is `tasks approve`/`deny`, which authenticate and read the
task to resolve its current policy step, so the previewed body is exact.

### Debug tracing

`--debug` (or `C1I_DEBUG=1`) traces each HTTP request to stderr — method, URL,
response status, and elapsed time, including every retry attempt. Headers and
bodies are never logged, so credentials don't leak. Output goes to stderr, so it
won't corrupt piped JSON on stdout:

```sh
$ c1i apps list --debug 2>trace.log
$ cat trace.log
> GET https://mycompany.conductor.one/api/v1/apps
< GET /api/v1/apps 200 OK (142ms)
```

## Authentication

```sh
# Browser-based login (OAuth device flow)
c1i auth login

# Or store credentials directly
c1i auth login --client-id <id> --client-secret <secret>

# Check credential status (also reports the storage backend)
c1i auth status

# Show the authenticated principal: user ID, display name, email, role/permission/feature counts
c1i auth whoami           # add --verbose for full roles/permissions/features arrays

# Remove stored credentials
c1i auth logout
```

### Credential sources

`c1i` reads credentials from the first source that has them, in this order:

1. **Environment variables** — set `C1I_CLIENT_ID` and `C1I_CLIENT_SECRET`
   (alongside `C1I_URL`) for non-interactive / CI use. Both must be set; if
   only one is set the value is ignored.
2. **OS keyring** — Keychain on macOS, Credential Manager on Windows, Secret
   Service (e.g. gnome-keyring, KeePassXC) on Linux. Used by default when
   available.
3. **File fallback** — a `0600` JSON file under your config directory
   (`~/.config/c1i/credentials/` on Linux, `~/Library/Application Support/c1i/credentials/`
   on macOS, `%AppData%\c1i\credentials\` on Windows). Used automatically when
   no OS keyring is available — typical on headless Linux servers, containers,
   CI runners, and WSL without a desktop environment.

`c1i auth login` writes to the OS keyring when it can and falls back to the
file backend transparently. `c1i auth status` tells you which source served
the active credentials.

## Shell Completion

```sh
# bash
c1i completion bash > /etc/bash_completion.d/c1i

# zsh
c1i completion zsh > "${fpath[1]}/_c1i"

# fish
c1i completion fish > ~/.config/fish/completions/c1i.fish
```

## Version

```sh
c1i version       # or: c1i --version
```

## Design

c1i is built specifically as a tool for AI agents:

- **Structured output**: All data commands produce NDJSON or JSON — never mixed or human-formatted output.
- **Self-documenting API**: `docs endpoints`, `docs endpoint`, and `docs search` let an agent discover and understand the C1 API without external documentation.
- **Predictable pagination**: List commands auto-paginate; `--page-token` gives manual control, and `--limit N` caps the total number of emitted results.
- **Raw API escape hatch**: `api --path` with `--paginate` lets an agent hit any endpoint, even ones without a native command. Paginated output uses the same NDJSON format as list commands.

## License

Apache 2.0
