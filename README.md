# c1i

**C1 Interface** — and it looks like `cli`. Get it?

> **Beta** — stabilizing toward 1.0. The core commands, flags, and output formats are stable; smaller changes may still land before 1.0.

A command-line interface for the [C1](https://www.conductorone.com) API designed for AI agents.
Structured output (NDJSON/JSON), built-in API docs, and auto-pagination.
For a human-friendly CLI, see [cone](https://github.com/ConductorOne/cone).

## Installation

Download signed macOS, Linux, and Windows releases with checksums, provenance, and SBOM attestations from the [official distribution center](https://dist.conductorone.com/ConductorOne/c1i).

```sh
# Homebrew
brew install conductorone/baton/c1i

# Go
go install github.com/ConductorOne/c1i@latest

# Container (image tags omit the leading "v" -- e.g. 0.5.2, not v0.5.2)
docker pull public.ecr.aws/conductorone/c1i:<version>
```

## Quick Start

```sh
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
c1i apps create --display-name <name> [--description <text>]
c1i apps owners <app-id> [--page-size N] [--page-token TOKEN] [--limit N]
c1i apps add-owner <user-id> --app-id <id>
c1i apps remove-owner <user-id> --app-id <id>
c1i apps set-owners <app-id> --user-id <id> [--user-id <id> ...] [--wait] [--wait-timeout 4m]
c1i apps delete <app-id>
```

`apps create` needs only `--display-name`; it makes a plain, unmanaged
container app — the zero state for "make an app, then register MCP servers
under it". The caller is auto-assigned as an owner, showing up in `apps owners`
after the usual provisioning lag. The new app comes back as pretty JSON under
an `app` key, and `--fields` is never applied to mutation output, so read the
new id from `.app.id` on the full object, not `.id`.

`apps delete` is a soft delete: the app is marked with `deletedAt` rather than
erased, which is why `apps list` rows carry a `deleted_at` field. Both commands
honor `--dry-run`.

`apps set-owners` returns as soon as the `PUT` is accepted. Pass `--wait` to
block and poll `GET .../ownerids` until every requested `--user-id` appears, or
`--wait-timeout` (default `4m`) elapses. A timeout exits `1` even though the
write itself was accepted — provisioning can simply still be in flight, so
re-check with `apps owners` rather than re-issuing the write. With `--dry-run`
the preview still only covers the `PUT`; `--wait` never polls.

`apps owners` is the read that reflects what `apps add-owner`,
`apps remove-owner` and `apps set-owners` write, and lags a write by roughly
45-150s (owner provisioning is asynchronous). It returns zero rows at exit 0
for a well-formed but nonexistent app id, so an empty result can also mean
"wrong id". `apps add-owner` and `apps remove-owner` change one owner at a
time; two `add-owner` calls issued at once were both observed to land.
`apps set-owners` replaces the whole list with exactly the ids you pass, so an
owner added between your read and your write is silently removed.

### Accounts

```sh
c1i accounts list --app-id <id> [--status enabled|disabled|deleted] [--type user|service_account|system_account] [--unmapped-only] [--query <text>] [--page-size N] [--page-token TOKEN] [--limit N]

c1i accounts set-owner <app-user-id> --app-id <id> --user-id <id>
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

# After a grant, wait for the set to stop changing rather than polling by hand
c1i grants list --app-id <id> --entitlement-id <id> --wait --wait-min 1
```

Grants are the bindings between accounts/users and entitlements. At least one
filter is required. Each NDJSON row includes the entitlement, the account
(`app_user_*`) and its `identity_user_id`, timestamps (`created_at`,
`deprovision_at`), and `grant_source_count` — `0` for a direct grant, or the
number of groups/roles the access is inherited through.

Grant provisioning is asynchronous, so a read taken moments after a grant or
revoke can catch the set mid-change. `--wait` re-reads every page every 5s and
prints nothing until the same grants come back `--wait-stable` times running
(default `3`, minimum `2` -- one read cannot show that anything held steady).
Progress goes to stderr, so stdout stays pure NDJSON. The 5s interval is fixed
and is not a flag; `--wait-stable` must fit inside `--wait-timeout` (default
`4m`), and a combination that cannot fit is rejected as a usage error rather
than left to time out.

**An empty result is stable.** A filter matching nothing settles at the first
opportunity -- about 10s at the defaults -- and exits `0` with zero rows.
Waiting on a grant you just made, that reads as "it did not happen" when the
truth is "not yet". Pass `--wait-min 1` (or the count you expect) to hold out
for that many grants and time out instead; exit `1` then means "did not
converge in time", not "absent". The default of `0` is deliberate:
empty-and-stable is the correct answer when you are waiting for a revoke --
and in that direction there is no flag that helps, since `--wait-min` is a
floor and cannot express a ceiling. `--wait` settles on whatever is steady, so
exit `0` still listing the row means "not yet, re-run", not "the revoke
failed".

`--wait` settles on the whole matching set, fetching every page on every poll
regardless of `--limit`; `--limit` only truncates what is printed. Filter
narrowly. `--wait` and `--page-token` are mutually exclusive.

A grant outlives the entitlement or account it points at, so rows also carry
`entitlement_deleted_at` and `app_user_deleted_at`: `jq
'select(.entitlement_deleted_at)'` finds grants whose backing object is gone.
Absent timestamps are `null`, never `""`.

### Tasks

```sh
c1i tasks list [--state open|closed] [--query <text>] [--assigned-to-me] [--page-size N] [--page-token TOKEN] [--limit N]
c1i tasks approve <task-id> [--policy-step-id <id>] [--comment <text>]
c1i tasks deny <task-id> [--policy-step-id <id>] [--comment <text>]
c1i tasks comment <task-id> --comment <text>
```

`approve`/`deny` target a specific policy step. If `--policy-step-id` is
omitted, the task's currently executing step is fetched and used automatically
for both — but `approve` requires a resolvable step and errors if it can't
find one, while `deny` proceeds without a step if none can be derived.

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
c1i functions usage <function-id> [--page-size N] [--page-token TOKEN] [--limit N]
```

`functions source` auto-resolves the function's published commit (falling back to its head/latest draft) and base64-decodes the source files. Without `--out-dir`, each file is printed to stdout with a `// ===== <name> =====` delimiter; with `--out-dir`, files are written to disk. Fetched source is developer-authored code that commonly inlines credentials, so files are written `0600` and the directory is created — or, if it already exists, tightened, never widened — to at most `0700` (owner-only; group access to the directory would buy nothing since the files inside are already unreadable to group/other, and a filename alone can be informative). Any setuid/setgid/sticky bit on the directory is stripped outright, since none of them mean anything once there is no group or other access left. A tightened pre-existing directory prints a warning to stderr naming its old mode. `functions usage` scans every automation and emits one row per step that calls the given function — useful before deleting a draft.

### Automations

```sh
c1i automations list [--enabled-only] [--calls-function <fid>] [--page-size N] [--page-token TOKEN] [--limit N]
c1i automations get <automation-id>
c1i automations executions list [--state done|error|pending|...] [--template-id <tid>] [--page-size N] [--page-token TOKEN] [--limit N]
```

Each `automations list` row includes `function_ids` (every distinct function the automation invokes), so `--calls-function` can answer "which automations call function X?". `executions list --state` accepts the short forms (`done`, `error`, `pending`, ...) or the full `AUTOMATION_EXECUTION_STATE_*` enum; state and template filtering are applied client-side, so pair a narrow filter with `--limit` to bound the work.

### Policies

```sh
c1i policies list [--page-size N] [--page-token TOKEN] [--limit N]
c1i policies get <policy-id>
c1i policies search [--query <text>] [--display-name <name>] [--policy-type grant|revoke|certify ...] [--include-deleted] [--exclude-policy-id <id> ...] [--page-size N] [--page-token TOKEN] [--limit N]
c1i policies create --display-name <name> --policy-type grant|revoke|certify [--description <text>] [--steps-file <file|-> ] [--rules-file <file|-> ] [--allow-deny-all]
c1i policies create --body-file <file|->
c1i policies update <policy-id> [--display-name <name>] [--description <text>] [--policy-type ...] [--steps-file <file|-> ] [--rules-file <file|-> ] [--allow-deny-all]
c1i policies update <policy-id> --body-file <file|-> --update-mask <paths>
c1i policies delete <policy-id>
c1i policies validate-cel <condition>
```

A policy describes how C1 processes a task: who approves it (an ordered list
of `policySteps`, each a oneof of approval/provision/accept/reject/wait/form —
the schema also declares `action`, which the server rejects as an unsupported
step type — and an approval step's approver is itself a oneof of ten arms:
users, manager, group, appOwners, self, entitlementOwners, expression, webhook,
resourceOwners, agent), and how `rules[]` route a task to one of several
step sequences by CEL condition. That structure is too deeply nested for a
flag surface, so `create`/`update` take it from a JSON file (or `-` for
stdin) via `--steps-file`/`--rules-file`/`--body-file` — the same pattern
`mcp servers register` uses for its auth config.

**Client-side guards** (`create` and `update`, before any request is sent,
exit code 2) exist because several C1 policy API defects are either silent
or return an opaque `HTTP 500` instead of a `400`:

- Empty/missing steps for a policy's baseline entry are refused —
  `POST /api/v1/policies` with no `policySteps` succeeds and silently
  returns a deny-everything policy (a single `{"reject":{}}` step), with no
  validation error. Pass `--allow-deny-all` if that's genuinely what you
  want (an explicit `steps:[]` is refused regardless — the server 500s on
  that, not a safe default).
- `--policy-type` unspecified, an empty `rules[].condition` (needs the
  literal `"true"` for a baseline/catch-all rule), a `provision` step,
  `fallback`/`fallbackUserIds` on an approver arm that doesn't support them
  (only `users`, `appOwners`, `webhook`, and `agent` lack it — the other six
  arms each support their own `fallback`/`fallbackUserIds`/
  `fallbackGroupIds`/`isGroupFallbackEnabled`), and `fallback:true` with
  nothing to fall back to (a bare server error that surfaces as `HTTP 500`).

`update` sends the API's required `{"policy": {...}, "updateMask": "..."}`
wrapper for you — a flat body 400s. The `--steps-file`/`--rules-file`
convenience flags derive the update mask from what you pass; `--body-file`
requires an explicit `--update-mask`.

`validate-cel` checks a CEL condition without creating or updating anything.
Its root variable is `subject` (not `user`); this validates the
`rules[].condition` environment specifically, which is NOT the same
environment `ExpressionApproval.expressions` run in (see the command's
`--help`). An invalid condition prints its compile markers and exits 2, so
`c1i policies validate-cel '<cond>' && ...` only continues on a condition
that compiles.

A soft-deleted policy still returns from a direct `get` (with `deletedAt`
populated) but disappears from `list` and the default `search`; only
`search --include-deleted` finds it there. List and search rows carry
`deleted_at` so deleted rows are distinguishable without a second call — it
is `null` on a live policy, so `jq 'select(.deleted_at)'` selects only the
deleted ones.

### MCP

Drive the MCP admin surface (servers, tools, toolsets, and bindings). Most commands take `--app-id`; `mcp servers` commands take the server's `<connector-id>` positionally, while tool/toolset/binding commands scope to a server with `--connector-id`.

```sh
# Servers (register, configure, and inspect MCP servers)
c1i mcp servers list               --app-id <id> [--page-size N] [--limit N]
c1i mcp servers get                <connector-id> --app-id <id>
c1i mcp servers search             --app-id <id> [--query <text>] [--tool-state approved|pending|disabled|removed] [--include-last-called-at] [--limit N]
c1i mcp servers register           --app-id <id> --type hosted   --display-name <name> --catalog-id <cid> [--source-app-id <id>] [--auth ... ] [--config-field k=v ...]
c1i mcp servers register           --app-id <id> --type external --display-name <name> --server-url <url> [--transport streamable-http|sse] [--auth ...]
c1i mcp servers update             <connector-id> --app-id <id> [--display-name <name>] [--description <text>] [--data-sensitivity ...] [--tool-prefix <p>] [--require-tool-approval]
c1i mcp servers update-credentials <connector-id> --app-id <id> --type hosted|external [--auth ...] [--update-mask <paths>]
c1i mcp servers delete             <connector-id> --app-id <id>
c1i mcp servers resync-tools       <connector-id> --app-id <id>   # EXTERNAL only; 400 on HOSTED
c1i mcp servers test-connection    (--server-url <url> [--transport ...] [--auth ...] | <connector-id> --app-id <id>)   # EXTERNAL only; 400 on HOSTED
c1i mcp servers discover-oidc      --issuer-url <url>
c1i mcp servers catalog list       [--query <text>] [--page-size N] [--limit N]
c1i mcp servers catalog get        <catalog-id>
c1i mcp servers connections list   [--page-size N] [--limit N]

# Tools
c1i mcp tools list    --app-id <id> --connector-id <id> [--page-size N] [--page-token TOKEN] [--limit N]
c1i mcp tools get     <tool-id> --app-id <id> --connector-id <id>
c1i mcp tools search  --app-id <id> --connector-id <id> [--query <text>] [--state ...] [--classification ...] [--page-size N] [--limit N]
c1i mcp tools approve <tool-id> --app-id <id> --connector-id <id> [--state approved|disabled|pending]
c1i mcp tools delete  <tool-id> --app-id <id> --connector-id <id>
c1i mcp tools history <tool-id> --app-id <id> --connector-id <id> [--page-size N] [--limit N]

# Toolsets (admin-curated tool groupings; one AppEntitlement per toolset)
c1i mcp toolsets list                   --app-id <id> --connector-id <id> [--page-size N] [--limit N]
c1i mcp toolsets get                    <toolset-id> --app-id <id> --connector-id <id>
c1i mcp toolsets create                 --app-id <id> --connector-id <id> --display-name <name> [--description <text>]
c1i mcp toolsets update                 <toolset-id> --app-id <id> --connector-id <id> [--display-name <name>] [--description <text>]
c1i mcp toolsets delete                 <toolset-id> --app-id <id> --connector-id <id>
c1i mcp toolsets get-by-entitlement     <app-entitlement-id> --app-id <id>
c1i mcp toolsets requestable-connectors <user-id>

# Bindings (which tools belong to which toolset)
c1i mcp bindings list     --app-id <id> --connector-id <id> --toolset-id <tid> [--page-size N] [--limit N]
c1i mcp bindings create   --app-id <id> --connector-id <id> --toolset-id <tid> --tool-id <id> [--tool-id <id> ...]
c1i mcp bindings delete   --app-id <id> --connector-id <id> --toolset-id <tid> --tool-id <id> [--tool-id <id> ...]
c1i mcp bindings by-tools --app-id <id> --connector-id <id> --tool-id <id> [--tool-id <id> ...]
c1i mcp bindings history  --app-id <id> --connector-id <id> (--toolset-id <tid> | --tool-id <id>) [--page-size N] [--limit N]

# Gateway (verify end to end: list and invoke tools over the live MCP gateway)
c1i mcp gateway list-tools [--full] [--gateway-url <url>]
c1i mcp gateway call <tool-name> [--args '{"k":"v"}'] [--gateway-url <url>]
```

**Auth for `register` / `update-credentials`:** convenience flags cover the simple methods — `--auth none`, `--auth bearer-token --bearer-token TOKEN`, `--auth custom-header --header-name NAME --header-value VALUE`, `--auth basic-auth --basic-auth-username USER --basic-auth-password PASS`. For OAuth2 / AWS SigV4 / Google service-account auth, pass the full config object via `--hosted-config-file` / `--external-config-file` (JSON file, or `-` for stdin) — generate a ready-to-edit skeleton with `--print-config-template --auth <method> [--type hosted]` instead of hand-writing it. Secrets are sealed server-side; reads only ever return `*_configured` booleans, never the values. `--token-sharing shared|per-user` sets the server's token-sharing mode (case-insensitive; `per_user`/`peruser` are also accepted). Per the register help, `per-user` is only valid with `oauth2` in authorization-code or passthrough mode, `bearerToken`, `customHeader`, or `basicAuth`. Note that a read-back can legitimately differ from what you sent: the backend may store a *resolved* OAuth2 grant such as `..._MODE_AUTHORIZATION_CODE` in place of the input mode, so that is a normal round-trip, not a bug. `--source-app-id` names the source app for a connector-backed HOSTED server.

`mcp tools approve` is the standard post-registration step: newly discovered tools (from `register` or `resync-tools`) start in `PENDING_REVIEW`, and an admin approves each one for the gateway to proxy calls. History endpoints return records newest-first.

**`mcp gateway`** closes the configure-then-verify loop: after registering a server and approving its tools, `list-tools` runs the MCP handshake against the live gateway and shows what's actually callable, and `call` invokes a tool and prints its result. The gateway URL defaults to the `-mcp` host derived from `--url` (e.g. `acme.conductor.one` → `acme-mcp.conductor.one/v1`); override with `--gateway-url`. Your standard C1 token is accepted by the gateway, so no extra auth is needed. `call` always prints the full result, but exits `7` (not `0`) when the result itself reports `isError: true` — the call succeeded, the tool didn't.

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

### Access profiles

An access profile controls which entitlements are requestable and who can
request them — admins use them to grant birthright access or to open access up
to a chosen audience.

**The API calls this object a request catalog**, and every path is
`/api/v1/catalogs`, so its JSON keys and ids say "catalog". The spec carries
both names — its `RequestCatalog` schema is tagged
`x-speakeasy-entity: Access_Profile` — so search for either.

Not to be confused with an app catalog, which is the per-user list of what one
user can request, derived from the access profiles they belong to.

```sh
c1i access-profiles list [--page-size N] [--page-token TOKEN] [--limit N]
c1i access-profiles get <access-profile-id>
c1i access-profiles create --display-name <name> [--description <text>] [--published] [--visible-to-everyone] [--request-bundle]
```

`access-profiles create` needs only `--display-name`. Every other flag is omitted from
the request body unless you pass it, so the server's own defaults apply; passing
`--published=false` explicitly still sends `false`. `--published` and
`--visible-to-everyone` both take effect at create time, so a catalog can be
created already published. The new catalog comes back as pretty JSON under
`requestCatalogView`, and `--fields` is never applied to mutation output, so read
the new id from `.requestCatalogView.requestCatalog.id`:

```sh
CAT_ID=$(c1i access-profiles create --display-name Engineering --published | jq -r .requestCatalogView.requestCatalog.id)
c1i access-profiles get "$CAT_ID"
```

Ordering matters once you gate a catalog that is *not* visible to everyone.
Adding a visibility binding (an access entitlement) to an unpublished catalog is
refused with a `400`, `catalog must be published to add an access entitlement`;
publishing it and repeating the same call succeeds. A catalog created with both
`--published` and `--visible-to-everyone` refuses them for a second reason —
`catalog is visible to everyone, cannot add access entitlements` — so create it
published but not visible to everyone if you intend to gate it.

`access-profiles list` rows do **not** carry a member count: the list endpoint reports
`memberCount` as `0` for every catalog while `access-profiles get` on the same id
reports a non-zero count, so the key is omitted from list rows. `access-profiles get`
also carries the catalog's `accessEntitlements` (its visibility bindings),
empty when there are none, which list rows omit.

There is no `access-profiles delete` command yet; use `c1i api --path
/api/v1/catalogs/<id> --method DELETE`. It is a soft delete, verified end to end:
the catalog leaves `access-profiles list`, while `access-profiles get` still returns it at exit
`0` with `deletedAt` set. Because deleted catalogs drop out of the list, a
`deleted_at` in a list row is null in practice; the field is kept to match
the sibling list rows that carry it, not as a signal to filter on.

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

# DELETE normally refuses a body; some endpoints (e.g. remove-membership)
# require one, so opt in explicitly
c1i api --path /api/v1/apps/<app>/entitlements/<ent>/remove-membership \
  --method DELETE --body '{"appUserId":"<app-user>"}' --allow-delete-body

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
file, or `-` for stdin) — the two are mutually exclusive. GET and DELETE refuse
a body by default (a body on either is more likely a mistake than intent); pass
`--allow-delete-body` to lift that for DELETE specifically, for the handful of
C1 endpoints that require one. `--query key=value` and
`--header key=value` are both repeatable. When `--paginate` is used, each page's first array-valued field is unwrapped and each item is emitted as a single line of NDJSON — the same format used by list commands. This covers both the canonical `list` key and typed keys like `automationExecutions`; use `--list-key <field>` to force a specific field. If the server returns the same `nextPageToken` twice in a row, `c1i` aborts with an error rather than looping forever. Without `--paginate`, the full JSON response is pretty-printed — and if that response carries a non-empty `nextPageToken`, `c1i` warns on stderr that the result is partial and names `--paginate`, since a truncated page is otherwise indistinguishable from a complete one at exit 0. stdout is unchanged either way.

### API Discovery & Documentation

The `docs` commands require no C1 credentials — agents can use them to explore the API before authenticating.

```sh
# Print the agent bootstrap doc: output contracts, exit codes, when to
# prefer first-class commands over raw API calls (write to a file with --output)
c1i docs agents [--output AGENTS.md]

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

# Print an embedded, task-oriented runbook (list names if omitted)
c1i docs guide
c1i docs guide register-mcp-server
```

`docs guide` is embedded static content (no network call), unlike `docs search` / `docs page` which hit the C1 documentation site. Guides ship in two families: registering and operating MCP servers (`register-mcp-server`, `assign-toolset-everyone`, `test-mcp-gateway`, `delegate-entitlement-provisioning`) and everyday app/access-request workflows (`configure-new-app`, `request-access`, `inspect-and-approve-task`). Run `c1i docs guide` with no argument for the full, current list.

`docs skill` is kept as an alias of `docs agents` for backward compatibility;
both print identical output.

## Output Conventions

- **List commands** (`users list`, `apps list`, etc.) output NDJSON (one JSON object per line).
- **`api`** outputs pretty-printed JSON. With `--paginate`, outputs NDJSON (one list item per line). A `200` whose body is not JSON is an error (exit `6`), not a silent pass-through — a `--path` that escapes the API prefix can reach the web app and return HTML, which used to print as though it had succeeded. An empty body still succeeds, since some endpoints answer a write with nothing.
- **`docs`** commands output NDJSON (`search`, `endpoints`), pretty JSON (`endpoint`, `openapi` is YAML), or plain text (`page`).
- List commands auto-paginate by default. Pass `--page-token` to fetch a single page manually.
- `--page-size` **requests** a per-call batch size (max 100; `mcp tools history` and `mcp bindings history` allow 200). It is not a guarantee: a page can contain more rows than you asked for, by an amount that varies per endpoint and per size — `apps list --page-size 10` returned 23 rows, `policies list` 12, `users list` exactly 10. A positive value below 5 usually returns 5, though `policies list` floors at 6 and `mcp servers catalog list` has no floor. `--page-size 0` means the server's default of 25, not "none". A value over the max is clamped by c1i rather than rejected. A negative `--page-size` or `--limit` is a usage error (exit 2), rejected before any request. Use `--limit N` for an exact total: it is enforced client-side, so it holds even when a page overshoots, and it stops auto-pagination once reached.

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
- Matches the emitted keys, trying an **exact match first**, then falling back
  to a **case- and separator-insensitive** match. So `--fields displayName`
  resolves whether the output uses `displayName` (single-object reads) or
  `display_name` (list rows); the output keeps the source key's own spelling.
- Single-object `get` commands print the resource itself, so `--fields id`
  yields `{"id": ...}` and `jq -r .id` works. The API wraps the resource under
  its own key (`app`, `function`, `userView.user`, ...); `get` unwraps that and
  keeps every other envelope key — `expanded` among them — as a top-level
  sibling. Do **not** write the wrapper into a path: `--fields function.id`
  no longer resolves, because there is no `function` key left.
- `c1i api` is a raw passthrough and still returns the envelope. There, and
  for any genuinely nested field, a name that doesn't match at the top level is
  also searched for deeper: the shallowest match wins, and a tie at the same
  depth resolves to the alphabetically first full path, deterministically.
- Mutation output (`apps create` returns `{"app": ...}`) also keeps the
  envelope, but is never projected at all — `--fields` cannot blank a success
  message, so no search happens there.
- A `--fields` spec that matches **nothing at all** in the response (a typo, or
  a field that truly doesn't exist) is a usage error (exit `2`), not a silent
  `{}`. This is a zero-match check only: `--fields id,dispalyName` (typo) still
  exits `0` and silently returns just `{"id": ...}` — the misspelled field is
  dropped with no error and no other signal that it didn't match anything. This
  is deliberate, not a gap in the check: `--fields`/`C1I_FIELDS` is a
  persistent, session-wide setting, so one spec is routinely applied across
  many differently-shaped responses; erroring on any unmatched name would make
  a session-wide `C1I_FIELDS` fail on every command whose response happens to
  lack one of the names. Double-check the spelling of every name you pass —
  the tool only catches getting *all* of them wrong.
  - On list commands and `api --paginate`, "the response" means the **whole
    result**, not each row: rows are decided and streamed out one at a time as
    they're fetched (never buffered — these commands can walk unbounded,
    multi-page results), but a row whose projection is empty is **skipped**
    rather than printed as `{}` — a field present on some rows but absent on
    others just means fewer rows are printed, not an error, as long as it
    matches at least one row anywhere. Only a spec that matches nothing in
    **every** row across every page is an error, with a message ("...matched
    no keys in any row of the response") distinguishable from the
    single-object case above. Because every row's own projection was already
    empty (and skipped) by the time that whole-result verdict is known,
    nothing is printed on stdout before the error — never rely on output
    length to detect success without checking the exit code. If you're piping
    to `jq` (`c1i ... | jq ...`), remember `$?` reads the pipe's last command,
    not `c1i`'s — the stderr message is the durable signal here, not the exit
    code you'd read from a naive `$?` after the pipe.
  - **Worst case: a `--fields` spec that matches nothing at all, paired with
    `--limit`, scans the collection to completion before erroring** — "nothing
    ever matched" can't be known short of exhausting every page, and a typo is
    the ordinary way to hit this. Live-measured: `tasks list --fields <typo>
    --limit 2` made 193 requests over ~41s before exiting `2` on a tenant with
    ~9,650 tasks; on a 35,000-row `entitlements list` that's minutes. This
    isn't specific to `--fields` — it's the same rule `accounts list
    --unmapped-only` and `functions usage` already follow for their own
    client-side filtering (a filter applied after the fetch can't bound the
    work when nothing matches, no matter what `--limit` says); `--fields`
    combined with `--limit` is one more instance of it, not a new behavior.
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
| `2` | usage error (bad flags or arguments, an empty id, an id the API redirects to a collection, or the API returned any `4xx` other than `401`/`403`/`404`/`408`/`429`/`499`) |
| `3` | not authenticated, or API returned `401`/`403` |
| `4` | API returned `404` (not found) |
| `5` | API returned `429` (rate limited — back off and retry) |
| `6` | C1 failed: the API returned `5xx`, a redirect chain never settled, or it answered `200` with a body that isn't JSON |
| `7` | `mcp gateway call` completed, but the tool itself reported an error (`isError: true` in its result) |
| `8` | a system beyond C1, or the MCP protocol layer, failed — an upstream connector was unreachable or errored, the gateway itself was unreachable (DNS failure, refused connection), or the gateway returned a protocol-level JSON-RPC error |

`6` and `8` are both "something remote broke," but they call for different
responses. `6` means C1 itself is failing, so retrying later is reasonable and
there is nothing to fix at your end. `8` means C1 answered fine and something
past it did not, so the same call will usually fail the same way: if a connector
is at fault, inspect it (`c1i mcp servers get <connector-id> --app-id <id>`, and
`mcp servers test-connection` for an EXTERNAL server) — it may be unreachable or
its credentials may have expired; if it's a protocol-level JSON-RPC error, that
indicates a version mismatch or a bug in c1i, worth reporting rather than
working around. Before this split both arrived as `6`, which made "C1 is down"
indistinguishable from "the Slack connector is down."

An **empty id argument** is a usage error, not a lookup: `c1i policies get ""`
exits `2` and sends nothing. Without that guard an empty id renders a trailing
empty path segment, which the API redirects to the collection endpoint — so the
command used to print the entire list and exit `0`. The same check applies to a
raw `api --path` ending in `/`.

Relatedly, the REST client is **selective about HTTP redirects**. It follows one
only when both hold: the target path is identical to what was requested (a
trailing-slash difference counts as a change), and the target host is in the same
trust scope as the request host — the same host differing only in scheme or port,
or a `label.`-prefix relationship in either direction with at least two labels,
which covers `apex ↔ www` canonicalization. Anything else — a different path, or
the same path on an unrelated host — is refused as an error (exit `2`) naming the
target.

Both halves matter. The path rule is what closed a real bug: an id of `/` or `.`
escapes to a path the API redirects to the collection, which turned a
single-object read into a full listing with exit `0`. The host rule is what keeps
your token safe: a followed redirect is re-authenticated, so an unrestricted
follow would hand your bearer token to whatever host the redirect named.

A chain of allowed redirects that doesn't settle within five hops fails as a
remote error (exit `6`) rather than looping.

This applies to every command built on the shared transport: the REST client,
the MCP gateway, and the login handshake, so the path and redirect guards,
`--debug` tracing, and `--max-retries` cover the gateway and login too, not just
REST commands. **None of those four** applies to the `docs` subcommands that
fetch — `docs search`, `docs page`, `docs openapi`, `docs endpoints`,
`docs endpoint` — which call Go's default HTTP client directly: no path or
redirect guard there, and `--debug` and `--max-retries` are both inert.

One narrower carve-out inside login: the device-flow token poll forces its own
retry count to zero, because RFC 8628's polling interval already *is* that
call's retry strategy and a second layer underneath would double the delays.
`--max-retries` still governs the rest of the handshake.

A bad id is the only cause of a refused `3xx` observed so far, which is why it
maps to exit `2` — a redirect on an otherwise well-formed request would not be
the caller's mistake, and would still report `2`.

Pass `--error-format json` (or `C1I_ERROR_FORMAT=json`) to get a machine-readable
error object instead of the default `Error: <msg>` line. For API errors it
includes the status, method, path, and response body:

```sh
$ c1i api --path /api/v1/apps/<nonexistent-id> --error-format json
{"body":{"code":5,"message":"not found (request-id: ...)"},"error":"API error: API GET /api/v1/apps/<nonexistent-id> returned 404: ...","method":"GET","path":"/api/v1/apps/<nonexistent-id>","status":404}
```

The `body` is embedded as JSON when the API returned JSON, otherwise as a string.

## Configuration

c1i requires a C1 **URL**.

**c1i requires `https`.** A URL with any other scheme is rejected (exit `2`); it
is not rewritten. Credentials embedded in the URL are dropped with a warning on
stderr, and an embedded password is never echoed.

The scheme may be omitted (`tenant.c1eu.ai`), in which case `https` is assumed.
The host is lower-cased, so `HTTPS://TENANT.C1EU.AI` and `tenant.c1eu.ai`
resolve identically, and a protocol-relative `//tenant.example` is handled.

Both `*.conductor.one` and `*.c1eu.ai` (EU) tenant domains are accepted — pass
whichever your tenant uses. Only the shape of the URL is checked, never the
domain, so a typo like `mycompany.conductor.on` is accepted here and surfaces
later as an authentication failure.

A bare name (`--url mycompany`) is rejected: with more than one tenant domain in
use it is ambiguous, and silently expanding it to `mycompany.conductor.one` would
point an EU tenant at the wrong region. The error names where the value came
from, which matters when it is a stale entry in `~/.c1i.yaml` rather than
something you just typed. A single-label host is fine as long as it arrives as a
URL (`https://c1-staging`), which is how an internal-resolver name is reached.

> If you previously authenticated with a mixed-case `--url`, your stored
> credential was keyed by that exact casing and is no longer found now that the
> host is normalized. Run `c1i auth login` once to re-store it.

Set it via (in order of precedence):

1. `--url` flag
2. `C1I_URL` environment variable
3. `~/.c1i.yaml` config file:

   ```yaml
   url: https://mycompany.conductor.one
   ```

These are equivalent:
- `--url https://mycompany.conductor.one`
- `--url mycompany.conductor.one`
- `--url MYCOMPANY.CONDUCTOR.ONE` (the host is lower-cased)

**Every command prints which tenant it's about to use when the URL came from
`~/.c1i.yaml`.** Nothing on the command line names the config file, so a
stale entry there sends a command to the wrong tenant with no visible sign
otherwise. `--url` and `C1I_URL` don't print this warning: both are explicit
choices made for that invocation, and warning on every normal use of
`C1I_URL` would just train you to stop reading it. The warning goes to
stderr, once per invocation (never once per page of a paginated list):

```
Warning: no --url flag given; targeting https://mycompany.conductor.one (from ~/.c1i.yaml)
```

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

1. `--max-retries N` flag (any command that reaches the C1 API)
2. `C1I_MAX_RETRIES` environment variable
3. Default: `4`

Set `--max-retries 0` to disable retries entirely. Non-retryable responses
(4xx other than 429, and 501/505) fail immediately.

This covers the token mint too: minting or refreshing the OAuth2 bearer
c1i authenticates with is itself a request, subject to the same 429 retry
(never 5xx/network, since it's a POST) and the same `--max-retries` budget.

Every request also gets a fixed timeout, per attempt (a retried request
gets a fresh budget, not a shrinking share of one deadline): 10 minutes
for a REST or MCP gateway request, 30 seconds for `auth login`'s
device-flow requests and the OAuth2 token mint/refresh above — tighter
because those are fast request/response exchanges, not the kind of call
that legitimately runs long. Neither is configurable. 10 minutes leaves
roughly 3x headroom over the longest request this CLI is known to make
(an MCP `tools/call` invoking a slow tool, observed at 182 seconds), so
there's no known case that needs a longer one.

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
required. The exceptions are `tasks approve`/`deny` (authenticate and read the
task to resolve its current policy step) and `requests create grant`/`revoke`
when `--user-id` is omitted (authenticate to resolve it to the caller) — both so
the previewed body is exact.

### Debug tracing

`--debug` (or `C1I_DEBUG=1`) traces each API HTTP request to stderr — method,
URL, response status, and elapsed time, including every retry attempt. Headers
and bodies are never logged, so credentials don't leak. Output goes to stderr,
so it won't corrupt piped JSON on stdout:

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

# Show the authenticated principal (principle/user ID, role/permission/feature counts, and
# display name + email when a best-effort secondary lookup succeeds) plus the resolved
# tenant: "tenant" (base URL) and "tenantSource" (flag/env/config)
c1i auth whoami
# --verbose swaps the summary for the raw introspect payload (full roles/permissions/
# features arrays, but no display name or email) -- a different projection, not a superset
c1i auth whoami --verbose

# Machine-readable "which tenant am I about to write to?" — the pre-write check
c1i auth whoami --url https://mycompany.conductor.one --fields tenant

# Mint a short-lived bearer token for driving raw API calls yourself
c1i auth token            # add --json for token type and absolute expiry (RFC3339)

# Remove stored credentials
c1i auth logout
```

`c1i auth token` prints just the access token, newline-terminated, so it
composes into `curl -H "Authorization: Bearer $(c1i auth token)" ...`. It is
never written to disk — a new one is minted per invocation.

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

`powershell` is also available. Each generator takes `--no-descriptions` to
emit a script that completes names only, without the per-command help text.

## Version

```sh
c1i version       # or: c1i --version
```

## License

Apache 2.0
