---
name: c1i
description: CLI for the C1 (formerly ConductorOne) identity security platform — manage users, apps, entitlements, tasks, access reviews, and more.
version: {{VERSION}}
required_bins:
  - c1i
---

# c1i for agents (v{{VERSION}})

You are an AI agent using `c1i`, the CLI for the C1 API. Read this once,
early — it covers what `--help` can't: getting the tenant right, output
contracts, exit codes, and when to reach for `c1i api`. Save it locally with
`c1i docs agents -o AGENTS.md`.

## Tenant and auth

Get this right first. A wrong tenant returns plausible-looking data with
exit 0 — nothing downstream tells you it was the wrong one.

`--url` (or `C1I_URL`) selects the tenant. With neither set, c1i falls back
silently to whatever `url:` names in `~/.c1i.yaml`.
Pass `--url` explicitly on every invocation instead of relying on an
exported `C1I_URL` — if it gets lost between shell calls (a fresh shell per
call is common), you fall through to the config file's tenant with no
warning, and every result after that is silently wrong.

Credentials resolve in this order: `C1I_CLIENT_ID` + `C1I_CLIENT_SECRET` env
vars (read-only — c1i never writes them), the OS keyring, then a `0600` file
used automatically where no keyring exists (headless Linux, CI, containers).
`c1i auth login` to authenticate; `c1i auth whoami` to confirm who and where
you are before doing anything else.

## Choosing a command

Prefer a first-class command (`users get`, `mcp servers register`, `grants
list`, ...) over `c1i api`. First-class commands auto-paginate to
completion, return typed errors mapped to the exit codes below, and document
destructive cascades in their own `--help`. `c1i api` gets none of that — it
sends exactly what you tell it, once, and hands back whatever the server
returns.

`c1i api` is the right tool when no first-class command exists yet. Two
known gaps: access reviews (`/api/v1/access_review*`) and the entitlement
*proxy binding* path (a different object from `mcp bindings` — see `c1i docs
guide delegate-entitlement-provisioning`). Otherwise, discover.

The cobra tree never drifts from what's implemented. Step down it with
`--help` at each level:

    c1i --help
    c1i mcp --help
    c1i mcp servers get --help

For task-oriented walkthroughs and the API surface:

    c1i docs guide
    c1i docs endpoints --filter TEXT
    c1i docs endpoint <path>

    c1i api --path /api/v1/access_reviews

A few wire conventions if you build a raw request: GET endpoints take
`page_size`/`page_token` as snake_case query params; POST search endpoints
take `pageSize`/`pageToken` (camelCase) in the body; response pagination is
always `nextPageToken`. List/search responses wrap items under `"list"` —
except the MCP admin endpoints (`mcp_tools`, `mcp_toolsets`,
`tool_bindings`), which use a resource-named key (`"tools"`, `"profiles"`,
`"bindings"`) instead. The UI's "campaign" is the API's access review — a
campaign ID from a URL is the access review `id` directly.

## Reading output

- List commands emit NDJSON: one object per line. Pipe to `jq`.
- Single-object reads emit pretty-printed JSON.
- Mutation confirmations (create/update/delete) are never field-projected —
  `--fields`/`C1I_FIELDS` can't blank a success message.
- Casing differs by mode: list rows are snake_case (`app_id`,
  `display_name`); single-object reads carry the API's own camelCase
  (`userView`, `displayName`). `--fields` matches either casing at any
  depth — `jq` doesn't, so check a row's actual keys before writing a filter.
- Values keep their real JSON types: booleans and numbers are never
  stringified, so `jq 'select(.enabled)'` and numeric comparisons behave.
- `--fields id,user.email` projects to just those dot-paths; `C1I_FIELDS`
  sets the same thing for the whole session.

## Exit codes

| Code | Meaning | Do this |
|---|---|---|
| 0 | success | — |
| 1 | generic / unclassified error | inspect the message |
| 2 | usage error (bad flags/args) | fix the invocation, don't retry as-is |
| 3 | not authenticated, or API `401`/`403` | re-authenticate (`c1i auth login`) |
| 4 | API `404` | stop — the resource/path doesn't exist |
| 5 | API `429` | back off and retry |
| 6 | remote failure: API `5xx`, or an upstream MCP connector failed | retryable, not necessarily your fault |
| 7 | `mcp gateway call` succeeded but the tool reported `isError: true` | inspect the printed result, not just the code |

Branch on the exit code, not stderr text. For `mcp gateway call`, a
JSON-RPC-level error (the call itself failed, not the tool) is reclassified:
`-32602`/`-32601` (bad tool/method name) exits 2; code `0` (an upstream
connector failure) exits 6; any other code exits 1.

## Pagination

List commands auto-paginate to completion by default — one invocation gets
every page. Pass `--page-token` to opt out and fetch a single page manually.
Don't write your own pagination loop.

## Before you mutate

`--dry-run` (or `C1I_DRY_RUN`) previews a mutating command's method, path,
and body without sending it.

Two things are irreversible in ways their `--help` doesn't make obvious:

- Calling an MCP gateway tool whose description marks it "requestable"
  files a real access-request task assigned to a human. It is not a dry
  read.
- `mcp servers delete` cascades: every toolset bound to that server, and
  each toolset's app entitlement, is soft-deleted with it. Anyone whose
  access came through one of those entitlements is affected.

## Things that will surprise you

- Owner and grant provisioning are asynchronous. A read immediately after a
  write can look like a silent no-op for roughly a minute (owners: ~60-90s;
  grants: up to a couple of minutes).
- A task's `outcome` field is omitted while it's still open; it only
  appears once the task closes.
- Entitlement ids are unique only within an app — some system-builtin
  entitlements reuse the same id across every app that has one. Always key
  on `(app_id, id)` together, never `id` alone.
- `mcp servers test-connection` returns `toolCount` as a JSON string, not a
  number. The `tool_count` in NDJSON list rows is a real number.

## Carry forward

Record tenant-specific facts you learn this session — app/connector ids,
which MCP servers are EXTERNAL vs HOSTED, the tenant URL — in your own
memory or a local `AGENTS.md`. Don't re-record c1i mechanics (exit codes,
output format, flag names); those ship with the binary.
