---
name: c1i
description: CLI for the C1 (formerly ConductorOne) identity security platform — manage users, apps, entitlements, tasks, access reviews, and more.
version: {{VERSION}}
required_bins:
  - c1i
---

# c1i for agents ({{VERSION}})

You are an AI agent using `c1i`, the CLI for the C1 API. Read this once,
early — it covers what `--help` can't: getting the tenant right, output
contracts, exit codes, and when to reach for `c1i api`. Save it locally with
`c1i docs agents -o AGENTS.md`.

## Tenant and auth

Get this right first. A wrong tenant returns plausible-looking data with
exit 0.

`--url` (or `C1I_URL`) selects the tenant. With neither set, c1i falls back
to whatever `url:` names in `~/.c1i.yaml`. It must be a full host —
`mycompany.conductor.one` or `mycompany.c1eu.ai` (EU) — and `https` is required:
a bare `mycompany` and a non-https scheme are both usage errors (exit `2`)
before any request is sent.
Every command prints a `Warning: no --url flag given; targeting <url> (from
~/.c1i.yaml)` line to stderr when the URL came from the config file —
`--url` and `C1I_URL` print nothing, since both are an explicit choice for
that invocation. Don't rely on catching it: pass `--url` explicitly on every
invocation instead of an exported `C1I_URL`. A fresh shell per call is
common, and an env var that gets lost between calls falls through to the
config file's tenant silently on stdout — the stderr warning only appears on
the call that already fell through, and a result piped straight into the
next step won't show it at all if you aren't reading stderr.

Credentials resolve in this order: `C1I_CLIENT_ID` + `C1I_CLIENT_SECRET` env
vars (read-only — c1i never writes them), the OS keyring, then a `0600` file
used automatically where no keyring exists (headless Linux, CI, containers).
`c1i auth login` to authenticate; `c1i auth status` to confirm which tenant
you're pointed at (it prints the base URL) and `c1i auth whoami` to confirm
which identity you're acting as (userId, principleId, email, displayName —
no tenant URL in its output) before doing anything else.

## Choosing a command

Prefer a first-class command (`users get`, `mcp servers register`, `grants
list`, ...) over `c1i api`. A first-class command paginates to completion
without being asked, and its `--help` spells out any destructive cascade
before you trigger one. With `c1i api` you build the request yourself — the
wire conventions below are yours to get right, and pagination is opt-in via
`--paginate`, so a list endpoint without it returns only the first page.

Both share the same error classification: `c1i api` surfaces the same typed
errors and the same exit codes as any other command, so the table below
applies either way.

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

    c1i api --path /api/v1/access_reviews --paginate

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
| 2 | usage error (bad flags/args, an empty id, or any API `4xx` other than `401`/`403`/`404`/`408`/`429`) | fix the invocation, don't retry as-is — but read the message first: an API `4xx` can also be a state rejection (e.g. `task is closed`) rather than a bad argument |
| 3 | not authenticated, or API `401`/`403` | re-authenticate (`c1i auth login`) |
| 4 | API `404` | stop — the resource/path doesn't exist |
| 5 | API `429` | back off and retry |
| 6 | C1 failed: API `5xx` or `408`, a `200` with a body that isn't JSON, or a redirect chain that never settles | retryable, not your fault |
| 7 | `mcp gateway call` succeeded but the tool reported `isError: true` | inspect the printed result, not just the code |
| 8 | a system beyond C1, or the protocol layer, failed — including a gateway that is unreachable (DNS failure, refused connection) | retrying the same call usually repeats it. For a connector failure, check the server with `mcp servers get <connector-id> --app-id <id>` and `mcp servers test-connection`; the upstream may be unreachable or its credentials expired. For a protocol error, it's a version mismatch or a c1i bug — report it, don't work around it |

Branch on the exit code, not stderr text. An empty id argument is a usage error
(`c1i users get ""` exits 2 without sending anything) — worth knowing if you
build ids programmatically, because it used to return the whole collection with
exit 0. An id of `/` or `.` is refused the same way: those escape to a path the
API redirects to the collection, and the REST client refuses a redirect that
changes the path, so you get exit 2 instead of a full listing that looks like a
successful read. It also refuses a same-path redirect to an unrelated host, since
a followed redirect carries your token. It does follow pure host/scheme
canonicalization. That guard is REST-only — `mcp gateway` calls go through a
different HTTP client that follows redirects normally.

`6` versus `8`: `6` means C1 itself failed, so waiting and retrying is sensible.
`8` means C1 answered and something past it did not — a connector is down, or
the protocol disagreed — so the same call will usually fail the same way. For
`mcp gateway call`, a JSON-RPC-level error (the call failed, not the tool) is
classified: `-32602` (invalid params, e.g. an unknown tool name) exits 2, since
that is caused by what you passed; `-32601`/`-32700`/`-32600` and an upstream
connector failure (code `0`) exit 8; a JSON-RPC error carrying no code at all,
and any other code, exit 1.

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
- `accounts list --unmapped-only` filters after each page is fetched, not
  server-side. With `--page-token` (which turns off auto-pagination) a page
  can come back empty while unmapped accounts exist further along.
- `functions usage` filters automations client-side by
  `callFunction.functionId`, same as `accounts list --unmapped-only` above.
  With `--page-token` a page can come back with zero rows while a matching
  automation exists on another page.
- A task's `outcome` field is omitted while it's unspecified, not while the
  task is open — a task can be `TASK_STATE_OPEN` and already carry a real,
  non-UNSPECIFIED outcome (e.g. a provisioning failure mid-flow). Use
  `state`, not the presence of `outcome`, to tell whether a task is still
  pending.
- Entitlement ids are unique only within an app — some system-builtin
  entitlements reuse the same id across every app that has one. Always key
  on `(app_id, id)` together, never `id` alone.
- `mcp servers test-connection` returns `toolCount` as a JSON string, not a
  number. The `tool_count` in NDJSON list rows is a real number.
- `mcp servers search` only includes `tool_count` when you pass
  `--tool-state`; the API doesn't compute a count without a state filter, so
  a filterless search omits the key rather than showing a 0 that would look
  identical to a server with no tools.

## Carry forward

Record tenant-specific facts you learn this session — app/connector ids,
which MCP servers are EXTERNAL vs HOSTED, the tenant URL — in your own
memory or a local `AGENTS.md`. Don't re-record c1i mechanics (exit codes,
output format, flag names); those ship with the binary.
