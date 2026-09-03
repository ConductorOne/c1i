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

`--url` means the tenant on every command, including the `mcp servers`
subcommands that also take an external server's address — that one is
`--server-url`.

Credentials resolve in this order: `C1I_CLIENT_ID` + `C1I_CLIENT_SECRET` env
vars (read-only — c1i never writes them), the OS keyring, then a `0600` file
used automatically where no keyring exists (headless Linux, CI, containers).
`c1i auth login` to authenticate; then `c1i auth whoami` before doing
anything else — it reports both the identity you're acting as (principleId,
plus userId when the principal has one; email and displayName only when a
best-effort secondary lookup succeeds, and never under `--verbose`, which is
a different projection, not a superset) and the tenant you're pointed at,
the latter machine-readably: `tenant` is the resolved base URL and
`tenantSource` is where that URL came from (`flag`, `env`, or `config`, the
last meaning it fell through to `~/.c1i.yaml`). Gate any write on it:

```sh
c1i auth whoami --url https://mycompany.conductor.one --fields tenant
```

Both keys are emitted only once the credentials are proven against that
tenant, so a failure exits nonzero with no tenant rather than naming a
target you can't reach. `c1i auth status` prints the same tenant as plain
text, plus which credential store served it.

## Global flags

Every command accepts these, each with an env-var twin. Set the env var to
apply it for a whole session; the flag wins for a single invocation.

| Flag | Env | What it does |
|---|---|---|
| `--url` | `C1I_URL` | tenant host — see above |
| `--fields` | `C1I_FIELDS` | comma-separated dot-paths to keep in JSON output — see "Reading output" |
| `--dry-run` | `C1I_DRY_RUN` | preview a mutating request's method, path, and body without sending it |
| `--debug` | `C1I_DEBUG` | trace API HTTP requests (method, URL, status, timing) to stderr |
| `--max-retries` | `C1I_MAX_RETRIES` | retries for transient API failures (`429`/`5xx`); `0` disables |
| `--error-format` | `C1I_ERROR_FORMAT` | `text` (default) or `json` |

`--error-format json` is the one worth setting by default if you parse
failures: instead of `Error: <prose>` on stderr you get one JSON object,
`{"error": ...}`, carrying `status`, `method`, `path`, and the response `body`
when the failure came from the API. Still branch on the exit code — the JSON is
for the detail, not the classification.

`--debug` and `--max-retries` cover `mcp gateway` as well as the REST
commands: the gateway client threads both into its bearer mint and its
JSON-RPC calls. On a `mcp gateway call` that hangs or fails oddly, `--debug`
is the fastest way to see which request stopped.

They do **not** reach the `docs` subcommands that fetch — `docs search`,
`docs page`, `docs openapi`, `docs endpoints`, `docs endpoint`. Those bypass
the shared transport for Go's default HTTP client, so `--debug` prints nothing
and `--max-retries` is ignored. Silent `--debug` output there means the flag
never reached that path, NOT that no request was sent — don't read it as
evidence either way when a `docs` command comes back empty.

Nor do those five call the same place, which matters for egress rules and for
why one can fail while another works: `docs openapi`, `docs endpoints` and
`docs endpoint` fetch `conductorone.com/docs/openapi.yaml` (cached 24h under
`~/.c1i/cache/`, so a run can return rows without sending a request at all),
while `docs search` and `docs page` call a third party — `api.mintlify.com` —
with a public client-side key.

## Choosing a command

Prefer a first-class command (`users get`, `mcp servers register`, `grants
list`, ...) over `c1i api`. A first-class command paginates to completion
without being asked, and its `--help` spells out any destructive cascade
before you trigger one. With `c1i api` you build the request yourself — the
wire conventions below are yours to get right, and pagination is opt-in via
`--paginate`, so a list endpoint without it returns a single page. It says so
when that happens: if the response carries a `nextPageToken` and you did not
pass `--paginate`, `c1i api` warns on stderr that the result is partial. Do
not treat exit 0 alone as "I got everything" — check stderr, or pass
`--paginate`.

Both share the same error classification: `c1i api` surfaces the same typed
errors and the same exit codes as any other command, so the table below
applies either way.

`c1i api` is the right tool when no first-class command exists yet. Known
gaps: access reviews (`/api/v1/access_review*`), the entitlement *proxy
binding* path (a different object from `mcp bindings` — see `c1i docs guide
delegate-entitlement-provisioning`), and the catalog sub-resources
(`/api/v1/catalogs/{id}/…`) plus catalog delete/update. Otherwise, discover.

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
`"bindings"`) instead. `--paginate` unwraps whichever field it finds, but pass
`--list-key <field>` to name it yourself rather than hand-rolling the loop when
auto-detection picks the wrong array. GET and DELETE refuse a body by default;
the few endpoints that need one on DELETE (e.g. `remove-membership`) want
`--allow-delete-body`. The UI's "campaign" is the API's access review — a
campaign ID from a URL is the access review `id` directly, and the UI's "access
profile" is the API's catalog: `c1i access-profiles list`, `/api/v1/catalogs`, whose
`RequestCatalog` schema is tagged `x-speakeasy-entity: Access_Profile` in the
spec. Search for both names.

## Reading output

- List commands emit NDJSON: one object per line — but with `--fields`/
  `C1I_FIELDS` set, a row whose projection matches nothing is skipped
  entirely (never printed as `{}`), so the line count can be less than the
  underlying result count. Pipe to `jq`.
- Single-object reads emit pretty-printed JSON.
- Mutation confirmations (create/update/delete) are never field-projected —
  `--fields`/`C1I_FIELDS` can't blank a success message.
- Casing differs by mode: list rows are snake_case (`app_id`,
  `display_name`); single-object reads carry the API's own camelCase
  (`displayName`, `createdAt`). `--fields` matches either casing at any
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
| 2 | usage error (bad flags/args, an empty id, or any API `4xx` other than `401`/`403`/`404`/`408`/`429`/`499`) | fix the invocation, don't retry as-is — but read the message first: an API `4xx` can also be a state rejection (e.g. `task is closed`) rather than a bad argument |
| 3 | not authenticated, or API `401`/`403` | re-authenticate (`c1i auth login`) |
| 4 | API `404` | stop — the resource/path doesn't exist |
| 5 | API `429` | back off and retry |
| 6 | C1 failed: API `5xx`, a `200` with a body that isn't JSON, or a redirect chain that never settles | retryable, not your fault |
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
canonicalization. `mcp gateway` is guarded the same way: it is built on
the same shared transport, which applies the empty-path and redirect checks
unconditionally.

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

`--page-size` is a request, not a promise: a page may come back with more
rows than you asked for. That is server behavior, not a c1i bug. How much
more varies per endpoint and per size, so treat any figure you measure as
true of that endpoint, at that size, today.

Three more traps in the same flag:

- Most endpoints won't return fewer than 5 rows however small a positive
  value you pass, but that is not universal: `policies list` floors at 6,
  and `mcp servers catalog list` has no floor.
- `--page-size 0` does not mean "no paging". The server substitutes its own
  default of 25, and the rows returned may then overshoot that.
- A value above the max is not an error: c1i clamps it and sends the max. A
  negative `--page-size` or `--limit` is a usage error — c1i rejects it
  before sending, at exit 2.

So never size a batch, count a result set, or infer "there are only N of
these" from `--page-size`. `--limit N` is the exact control: c1i enforces
it client-side, so it holds whatever the server returns, and it stops
auto-pagination once reached.

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

One command sends more than one write: `entitlements create` POSTs a resource
type, a resource, then the entitlement, skipping the steps whose id you supply
via `--resource-type-id`/`--resource-id`. Its `--dry-run` previews all three.
There is no rollback, so a failure part-way through leaves the earlier objects
behind; the error names them, the flags that reuse them, and the create-only
flags the retry has to drop, and re-running without those flags creates
duplicates. Only a `CUSTOM` resource type can repeat on one app: a second
`--resource-type` of any other kind fails with a 500 (exit `6`, though
retrying never helps) saying `app resource type already exists` -- reuse the
existing type with `--resource-type-id` and drop both `--resource-type` and
`--resource-type-display-name`; either alongside the id is exit 2. Reusing a
resource with `--resource-id` likewise means you drop
`--resource-display-name`.

## Things that will surprise you

- Owner and grant provisioning are asynchronous. A read immediately after a
  write can look like a silent no-op for a couple of minutes (owner writes
  observed at 45-150s across set-owners, add-owner, remove-owner and the
  owner "apps create" assigns; grants: up to a couple of minutes). Verify
  owners with `c1i apps owners <app-id>`, not `apps get`'s `appOwners`
  field, and don't wait for that field to fill — it read [] on every app
  checked, including all those `apps owners` reported
  owners for. An empty `appOwners` is not evidence an app has no owners.
  `apps owners` also
  returns zero rows at exit 0 for a well-formed but nonexistent app id, so an
  empty result is either "no owners" or "wrong id"; `apps add-owner` on the
  same id exits 4. Don't write your own poll loop for this: `apps set-owners`
  takes `--wait` (with `--wait-timeout`, default `4m`) and blocks until every
  requested owner appears. A `--wait` timeout exits `1` and does not mean the
  write failed — provisioning may still be in flight, so re-check with
  `apps owners` instead of re-issuing the write.
- `grants list --wait` can report success with zero rows. An empty result is
  stable, so a filter matching nothing settles in ~10s and exits `0` -- which
  looks identical to "the grant did not happen" but usually means "not yet".
  After a write, pass `--wait-min 1`; exit `1` then means "did not converge in
  time", not "definitely absent". Without a minimum, treat `--wait` plus zero
  rows as inconclusive, never as a negative answer. The reverse has no flag:
  `--wait` settles on whatever is steady, and an undeprovisioned grant is
  steady, so after a revoke exit `0` still listing the row means "not yet,
  re-run", not "the revoke failed".
- `grants list --wait` buffers: nothing reaches stdout until the set settles,
  unlike every other list command. Progress goes to stderr, so stdout stays
  pure NDJSON, and a timeout exits `1` printing no rows. `--wait` fetches every
  page on every poll regardless of `--limit`, which only truncates what is
  printed, and it is mutually exclusive with `--page-token`.
- `--wait-stable` counts consecutive identical reads (default `3`, minimum
  `2`). Two is not enough: a pause mid-change is indistinguishable from
  completion. Three is a heuristic, not a proof -- size it past the longest
  pause you have actually observed. The 5s poll interval is fixed, not a flag.
- `accounts list --unmapped-only` filters after each page is fetched, not
  server-side. With `--page-token` (which turns off auto-pagination) a page
  can come back empty while unmapped accounts exist further along.
- `functions usage` filters automations client-side by
  `callFunction.functionId`, same as `accounts list --unmapped-only` above.
  With `--page-token` a page can come back with zero rows while a matching
  automation exists on another page.
- Same rule, worse case: a `--fields`/`C1I_FIELDS` spec that matches nothing
  anywhere, combined with `--limit`, scans the whole collection before
  erroring exit `2` — like `--unmapped-only` above, a post-fetch filter can't
  bound the work when nothing has matched yet. A typo is the ordinary way to
  hit this. Measured: `tasks list --fields <typo> --limit 2` made 193
  requests over ~41s on a ~9,650-row tenant; a 35,000-row `entitlements list`
  would take minutes. No cap exists for this on purpose — a first-page-only
  check would false-error on a real field that's just sparse.
- A task's `outcome` field is omitted while it's unspecified, not while the
  task is open — a task can be `TASK_STATE_OPEN` and already carry a real,
  non-UNSPECIFIED outcome (e.g. a provisioning failure mid-flow). Use
  `state`, not the presence of `outcome`, to tell whether a task is still
  pending.
- The `/api/v1/tasks/{id}/action/*` endpoints echo the task as it was *before*
  the action. Live: closing an open task returned `TASK_STATE_OPEN`, and
  restarting a closed one returned `TASK_STATE_CLOSED`. `tasks
  close`/`reassign` therefore never print a state (`close` reports `task_id`,
  `reassign` also the `policy_step_id`); if you call these actions
  through `api`, read the task back rather than trusting the response's
  `state`.
- Entitlement ids are unique only within an app — some system-builtin
  entitlements reuse the same id across every app that has one. Always key
  on `(app_id, id)` together, never `id` alone.
- `POST /api/v1/apps/{app_id}/entitlements` requires `appResourceTypeId` and
  `appResourceId` even though its OpenAPI schema lists only `displayName` as
  required; omitting them 400s on the id regex. `entitlements create` handles
  this for you.
- `mcp servers test-connection` returns `toolCount` as a JSON string, not a
  number. The `tool_count` in NDJSON list rows is a real number.
- `mcp servers search` only includes `tool_count` when you pass
  `--tool-state`; the API doesn't compute a count without a state filter, so
  a filterless search omits the key rather than showing a 0 that would look
  identical to a server with no tools.
- `access-profiles list` rows carry no member count on purpose. The list endpoint
  reports `memberCount` as `0` for every catalog while `access-profiles get` on the
  same id reports a non-zero count, so the key is dropped rather than emitted
  as a zero that reads like "no members". Use `c1i access-profiles get <access-profile-id>`
  for the count, and for the catalog's `accessEntitlements` (always present,
  empty when there are none), which list rows also omit.
- A catalog's visibility bindings can only be added after it is published:
  `POST /api/v1/catalogs/{id}/visibility_bindings` on an unpublished catalog
  is a `400`, `catalog must be published to add an access entitlement`; on one
  created with `--visible-to-everyone` it is a `400`,
  `catalog is visible to everyone, cannot add access entitlements`. A
  catalog published but not visible to everyone accepts them immediately.
- There is no `access-profiles delete` yet; delete via `c1i api --path
  /api/v1/catalogs/<id> --method DELETE`. It is a soft delete: the catalog
  leaves `access-profiles list`, while `access-profiles get` still returns it at exit `0`
  with `deletedAt` set. So a `deleted_at` in an `access-profiles list` row is null in
  practice — don't read the null as "not deleted", check with a get.

## Carry forward

Record tenant-specific facts you learn this session — app/connector ids,
which MCP servers are EXTERNAL vs HOSTED, the tenant URL — in your own
memory or a local `AGENTS.md`. Don't re-record c1i mechanics (exit codes,
output format, flag names); those ship with the binary.
