# c1i for agents (v{{VERSION}})

You are an AI agent using `c1i`, the CLI for the C1 API. This doc covers the
conventions `--help` doesn't: output contracts, exit codes, and which command
to reach for. Save it locally with `c1i docs agents -o AGENTS.md`.

## Discovery

The cobra command tree is the source of truth — it never drifts from what's
actually implemented:

    c1i --help
    c1i <group> --help
    c1i <group> <command> --help

For task-oriented walkthroughs, use `c1i docs guide` (lists available names)
and `c1i docs guide <name>`. For the full command reference in one file, use
`c1i docs skill`.

## Prefer first-class commands over raw API

Reach for a first-class command (`users get`, `mcp servers register`, `grants
list`, ...) before `c1i api`. First-class commands auto-paginate to
completion, return typed errors that map to the exit codes below, and spell
out destructive cascades in their `--help`. `c1i api` gets none of that: it
sends exactly what you tell it to, once, and surfaces whatever the server
returns.

`c1i api` is still the correct tool when no first-class command exists yet for
an endpoint. Find those gaps with `c1i docs endpoints` (list) and `c1i docs
endpoint <path>` (full request/response schema) before falling back to:

    c1i api --path /api/v1/some/endpoint --method POST --body '{"...": "..."}'

## Output contract

- List commands emit NDJSON: one JSON object per line. Pipe to `jq`.
- Single-object reads emit pretty-printed JSON.
- Mutation confirmations (create/update/delete) are never field-projected —
  `--fields` / `C1I_FIELDS` can't blank a success message.
- Values keep their real JSON types: c1i never stringifies a boolean or a
  number, so `jq 'select(.enabled)'` and numeric comparisons behave.
- Casing differs by output mode: list rows are snake_case (`app_id`,
  `display_name`); single-object reads pass the API's own camelCase through
  (`userView`, `displayName`). `--fields` accepts either casing at any depth,
  so you don't have to know which — but `jq` does, so check a row first.
- One wire quirk to know: `mcp servers test-connection` returns `toolCount`
  as a JSON string, not a number.
- `--fields id,user.email` (comma-separated dot-paths) projects output to
  just those keys. `C1I_FIELDS` sets the same thing session-wide — if output
  looks unexpectedly sparse, check whether it's set.

## Exit codes

| Code | Meaning | Do this |
|------|---------|---------|
| 0 | success | — |
| 1 | generic / unclassified error | inspect the message |
| 2 | usage error (bad flags/args) | fix the invocation, don't retry as-is |
| 3 | not authenticated, or API `401`/`403` | re-authenticate (`c1i auth login`) |
| 4 | API `404` (not found) | stop; the resource/path doesn't exist |
| 5 | API `429` (rate limited) | back off and retry |
| 6 | remote failure: API `5xx`, or an upstream MCP connector failed | retryable, but not immediately your fault |
| 7 | `mcp gateway call` succeeded as a call, but the tool itself reported `isError: true` | inspect the printed result, not the exit code alone |

Branch on the exit code, not stderr text.

## Pagination

List commands auto-paginate to completion by default — you get every page in
one invocation. Pass `--page-token` to opt out and fetch a single page
manually. Don't write your own pagination loop; you don't need one.

## Before you mutate

`--dry-run` (or `C1I_DRY_RUN`) previews a mutating request's method, path,
and body without sending it. Use it to check what an invocation would do
before committing to it.

Two operations are irreversible in ways that aren't obvious from their
`--help` text:

- Calling an MCP gateway tool whose description marks it "requestable"
  files a real access-request task assigned to a human. It is not a dry
  read.
- `mcp servers delete` cascades: every toolset bound to that server, and
  each toolset's app entitlement, is soft-deleted with it. Anyone whose
  access came through one of those entitlements is affected.

## Auth

Credentials resolve in this order: `C1I_CLIENT_ID` + `C1I_CLIENT_SECRET`
env vars (read-only — c1i never writes them), the OS keyring, then a 0600
file used automatically where no keyring exists (headless Linux, CI,
containers). Log in with `c1i auth login`; confirm with `c1i auth whoami`.
`--url` / `C1I_URL` selects the tenant. See `c1i auth --help`.

## Persist what you learn

Record tenant-specific facts you discover during a session — app IDs,
connector IDs, which MCP servers are EXTERNAL vs HOSTED, the tenant URL — in
your own persistent memory or a local `AGENTS.md`. Don't re-record c1i
mechanics (exit codes, output format, flag names); those ship with the
binary and are covered by `docs agents` and `docs skill`.
