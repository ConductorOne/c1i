---
name: c1i
description: CLI for the C1.ai identity security platform — manage users, apps, entitlements, tasks, access reviews, and more.
version: {{VERSION}}
required_bins:
  - c1i
---

# c1i for agents ({{VERSION}})

You are an AI agent using `c1i`, the CLI for the C1.ai API. Read this once,
early — it covers what `--help` can't: getting the tenant right, output
contracts, exit codes, and when to reach for `c1i api`. Save it locally with
`c1i docs agents -o AGENTS.md` (`-o -` is stdout). For the full reference, run `c1i docs readme`;
`c1i docs changelog` prints the migration notes (both embedded, no network).

## Tenant and auth

Get this right first. A wrong tenant returns plausible-looking data with
exit 0.

`c1i` resolves the tenant in this order: `--url`, `C1I_URL`, then `url:` in
`~/.c1i.yaml` (never read with a relative `HOME`). It must be a full host —
`example.conductor.one` or `example.c1eu.ai` (EU) — and `https` is
required. An explicit `:443` is dropped (another port is kept) and a path, query
or fragment is ignored. A bare `example`, a non-https scheme, and a malformed host (an embedded space
or control character, or a stray scheme like `://host`) are all usage errors
(exit `2`) before any request is sent.
Every command that resolves a C1 tenant URL prints a `Warning: no --url flag
given; targeting <url> (from ~/.c1i.yaml)` line to stderr when the URL came from
the config file; `--url` and `C1I_URL` print nothing, since both are an explicit
choice for that invocation. Don't rely on seeing it: pass `--url` explicitly on
every invocation, because an exported `C1I_URL` can be lost between calls (a
fresh shell per call is common) and the result then falls through to the config
file's tenant silently on stdout.

`--url` means the tenant on every command, including the `mcp servers`
subcommands that also take an external server's address — that one is
`--server-url`.

Credentials resolve in this order: `C1I_CLIENT_ID` + `C1I_CLIENT_SECRET` env
vars (read-only — c1i never writes them), the OS keyring, then a `0600` file
used automatically where no keyring exists (headless Linux, CI, containers).
With a relative `HOME` and no keyring, that file fallback errors rather than
write under the working directory, so every command that needs stored
credentials fails: `auth login`, `auth logout`, and any authenticated command
run without the env vars.
Only the bearer c1i attaches automatically for REST commands is cached and
reused across invocations until it nears expiry, so a run of one-shot commands
does not write a `client_credentials` audit event each time. It uses the OS
keychain when available; on Unix-like hosts without a usable keyring, it uses
a hardened `0600` file fallback. `auth token` and `mcp gateway` always mint a
fresh bearer and never persist it.
`C1I_NO_TOKEN_CACHE=1`/`true` disables the REST-client cache (`0`/`false` keep
it; any other value exits 2). Exactly one of `C1I_CLIENT_ID`/`C1I_CLIENT_SECRET`
set also exits 2; `auth login` and `auth logout` still run and warn on stderr.
`c1i auth login` to authenticate; then `c1i auth whoami` before doing
anything else — it reports both the identity you're acting as (principleId,
plus userId when the principal has one; email and displayName only when a
best-effort secondary lookup succeeds, and never under `--verbose`, which is
a different projection, not a superset) and the tenant you're pointed at,
the latter machine-readably: `tenant` is the resolved base URL and
`tenantSource` is where that URL came from (`flag`, `env`, or `config`, the
last meaning it fell through to `~/.c1i.yaml`). Gate any write on it:

```sh
c1i auth whoami --url https://example.conductor.one --fields tenant
```

Both keys are emitted only once the credentials are proven against that
tenant, so a failure exits nonzero with no tenant rather than naming a
target you can't reach. `c1i auth status` prints the same tenant as plain
text, plus which credential store served it.

To log in with less than your full access, run
`c1i auth login --scoped-role basic-user` (repeatable; a role name or ID from
`c1i roles list`). There is no prompt without a terminal, but a human must
still approve the device code; in a terminal with nothing stored, login first
asks whether to scope, which `--choose-roles=false` skips. A name you can't
scope to exits 2 and lists the valid names; an unknown ID exits 4. The
credential gets `403` (exit 3) on commands its roles don't cover. An error
naming a "temporary credential" (exit 1) means login couldn't delete the helper
it used to read roles; your own credential may still have been stored, so check
`c1i auth status`, and delete the helper in C1.ai.

## Global flags

Every command accepts these, each with an env-var twin. Set the env var to
apply it for a whole session; the flag wins for a single invocation.

| Flag | Env | What it does |
|---|---|---|
| `--url` | `C1I_URL` | tenant host — see above |
| `--fields` | `C1I_FIELDS` | comma-separated dot-paths to keep in JSON output — see "Reading output" |
| `--dry-run` | `C1I_DRY_RUN` | preview a mutation (a C1 REST write, or `upgrade`) as one JSON object (`.requests[]`: method, URL, body); it never has an `id`, and credential fields print as `[redacted]` (`upgrade --dry-run` adds an `.upgrade` object); unsupported for `mcp gateway call`, `auth login` and `auth logout` |
| `--debug` | `C1I_DEBUG` | trace API HTTP requests (method, URL, status, timing) to stderr; a URL's username (except `git`), password and secret-named parameter values (names ending in `token`, including pagination cursors such as `page_token`; query, fragment and `;` path parameters) print as `[redacted]`, as in `--dry-run` |
| `--max-retries` | `C1I_MAX_RETRIES` | retries for transient API failures (`429`/`5xx`); `0` disables; negative exits 2 (flag, env and config alike) |
| `--error-format` | `C1I_ERROR_FORMAT` | `text` (default) or `json` |

A non-empty env value that doesn't parse (`C1I_DRY_RUN=yes`,
`C1I_MAX_RETRIES=abc`) is a usage error naming the variable, before any
request; an empty one means unset. A flag set on the command line overrides
its env var entirely, so a bad env value is then ignored. The same keys in
`~/.c1i.yaml` (`dry_run`, `debug`, `max_retries`, `error_format`) are validated
alike, naming the file and key. A `~/.c1i.yaml` that exists but can't be read
or parsed exits 2 naming the file, even with `--url` (a missing one is fine; `docs`, `help` and
`version` ignore it).

In text mode, control characters and invalid UTF-8 from the server print on
stderr as visible escapes (`\x1b`, `\u202e`); `\n` and `\t` are kept.

`--error-format json` is the one worth setting by default if you parse
failures: stderr becomes NDJSON. Error objects carry `"error"` (select them
with `jq 'select(.error)'`) plus `status`, `method`, `path`, and the response
`body` when the failure came from the API (masked: a secret-named parameter or field
value is replaced by `[redacted]` where it sits in a value position, every other byte as
sent; a whole-JSON body with a mask is re-encoded compactly, one with nothing masked is unchanged; if a secret cannot be placed in the original bytes, the body falls back to its decoded text with every secret masked); notices carry `"notice"` and a `"kind"` (such as `dryRun`), `--wait`
progress `"progress"`, and `--debug` lines `"debug"`. A write command's error
also carries `"applied"`: `true`, `false`, or `"unknown"` (the request may
have reached the server, including when a retried attempt was sent before the
final failure); `api` carries none. `mcp tools approve` writes one error
object per failed id (with `"id"`) and a final summary with no `id`, carrying
`succeeded`, `failed` and `notAttempted`. Still branch on the exit code — the
JSON is for the detail, not the classification.

`--debug` and `--max-retries` cover `mcp gateway`, `auth login` (except its
device-code polling, which never retries), `upgrade` and every `docs` command
as well as the REST commands: the gateway client threads both into its bearer
mint and its JSON-RPC calls. On a
`mcp gateway call` that hangs or fails oddly, `--debug` is the fastest way to
see which request stopped.

A `docs search` or `docs page` failure (an unreachable docs site, a non-200,
an unparseable body) exits `8`, whatever the status.

Nor do the fetching `docs` commands call the same place, which matters for
egress rules and for why one can fail while another works: `docs openapi`,
`docs endpoints` and `docs endpoint` fetch
`https://www.c1.ai/api/openapi.yaml` (cached 24h at
`~/.c1i/cache/api-openapi.yaml`, so a run can return rows without sending a
request), while `docs search` and `docs page` call a third party —
`api.mintlify.com` — with a public client-side key. `docs search` is semantic
with no relevance threshold, so it always returns up to 10 plausible hits even
for a nonsense query: never read a result as proof a concept exists, or an
unexpected result as proof it is absent. `docs endpoints --filter` DOES have a
true no-match. If a spec fetch fails, an expired cache is served with a
one-line `Warning:` on stderr naming its age; with no cache, it exits 4 for a
404, 5 for a 429, 6 for a 5xx, and 8 otherwise. With a cache
to fall back on, the fetch makes one attempt unless you set `--max-retries` or
`C1I_MAX_RETRIES`.

Don't store files in `~/.c1i/cache/`: a spec refresh deletes any file there
that the running version doesn't use.

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

Two families need a workflow entry point rather than id lookup alone:
`findings` (13 verbs) mutates through `--body-file` JSON; its `bulk-state` and
`bulk-create-tasks` are asynchronous and print a `bulkActionId` c1i cannot read
back, and `create-task --policy-id` looks the policy up first (unknown exits 4).
`role-mining config update --body-file` REPLACES the whole config, and
`role-mining access-profiles create --body-file` is a multi-operation write;
read the current config/profile and verify afterwards.

`c1i api` is the right tool when no first-class command exists yet.
Continuous audit-log export is `export exporters` (create, update, delete,
`--datasource-id`, `--prefix`, `--export-id`; `export exporters events
<export-id>` lists what one delivered), and `export events` reads the bulk
System Log directly (`--since/--until` RFC3339, `--since-event-uid` to resume,
`--sort`; OCSF NDJSON); saved reports are `reports` (`list|get|update|delete|run`,
`program --program-id`, `provenance <run-id> --report-id`, `update --prompt`).
There is no `reports` create: the API saves one only from a rendered surface.
Access-profile lifecycle, requestable and visibility entitlements, and bundle
automation are first-class under `access-profiles`; use its nested `--help` to
choose a command. Use `entitlements proxy-bindings` for directional
entitlement-to-entitlement links, `access-reviews` for campaign lists,
lifecycle calls (`delete` included), reports, and a campaign's scope and
entitlement selection (`scope get|set|set-excluded|set-by-resource-type`,
`entitlements add|remove`, `excluded-entitlements add|remove`; an unknown
`--entitlement-id` or body-file reference exits 4 (a soft-deleted one 2)
before the send; the API's
`scope_by_excluded_resource_type` route is "superseded and scheduled for
removal" and has no command), `access-reviews templates` for reusable campaign
templates (`get|create|update|delete` plus the same scope commands; no route
lists templates, so take the id from `create` or a campaign's
`accessReviewTemplateId`; `create` takes `--display-name`, `--policy-id`,
repeatable `--owner-id`, `--description`, `--review-instructions` and
`--scope-type`, or `--body-file` for schedule fields; owner and policy ids are
looked up first, an unknown one exits 4), and `mcp classifiers` for AI-governance
classifiers, bindings, templates, the singleton agent policy, and tool gates.
Classifier creates and updates take JSON because their nested, ordered rules
must be preserved; read first and use the explicit `--update-mask` for updates.
`--update-mask` paths are camelCase, sent as given; an empty path or dot-segment
exits 2, and on `mcp servers update-credentials`/`test-connection` an explicit
mask must name every config key the command sends (see README "Update masks").

Tenant-wide settings are first-class under `settings` (README "Settings"):
`ai-governance`, `session`, `sso`, `cross-app-access`, `contacts` and `request`
have `show` and a masked `update` whose flags derive the mask
(`--default-tool-classification`, `--audit-verbosity`, `--auto-discovery`,
`--discovery-interval`, `--prefer-code-mode`, `--surface-requestable-tools`,
`--untrusted-judge-disable`, `--allowed-client-type`, `--inactivity-hide-after`,
`--inactivity-close-after`, `--retention-delete-after`; `--max-session-length`,
`--enable-idle-timeout`, `--idle-timeout`, `--external-clients-enabled`,
`--client-id-metadata-document-policy`; `--enabled`, `--default-subject-type`,
`--default-assertion-lifetime`, `--default-id-token-alg`;
`--default-grant-lifetime`, `--allow-refresh-token-subjects`,
`--default-signing-algorithm`, `--enabled-signing-algorithm`,
`--xaa-id-token-lifetime`; `--security-email`, `--billing-email`,
`--operations-email`; `--skip-justification`,
`--max-bulk-entitlement-selection`), or `--body-file` with `--update-mask` for
any other field. A repeatable list flag replaces the whole list; to clear a
field, mask it and leave it out of the body. The singletons have no id, so
read the fields you need. `settings ai-governance|sso|cross-app-access history`
stream change records; `settings aws-external-id show` prints the IAM trust
external id; `settings session test-source-ip --ip --allow-cidr`
checks an IP against CIDRs you give it and exits 0 for either answer (read
`.allowed`). `settings domains set --domain` REPLACES the whole domain set:
list first and pass every domain to keep (`--body-file` needs a `newDomains`
array; `[]` empties the set). `settings notifications org|user update` and
`findings settings edge update` take the whole request in `--body-file` with
no mask, so a body that names nothing (no non-empty `channelSettings`, or
neither `settings` nor `subcategorySettings`) exits 2 unsent. In the run
checked, a partial notifications body on either scope changed only the
settings it named (one of 86 leaves on org). The email provider and identity-policy defaults have no command.

Role ids for `service-principals credentials create --scoped-role`,
`service-principals trusts create|update --scoped-role` and
`auth login --scoped-role` come from `c1i roles list` (NDJSON; README lists the
fields); `c1i roles get <role-id>` adds a role's `serviceRoles` and
`permissions`.

Every flag sent to the API as a duration (`--duration-grant`, `--duration`,
`--expires`, `--callback-timeout`, the `settings ... update` lifetimes and
intervals) takes a whole-number Go (`8h`, `1h30m`) or
protobuf (`28800s`) duration, with no sign and no decimals, and is sent in
seconds. Anything else (`-1h`, `+1h`, `1.5h`, `01h`, empty) exits `2` before
sending; zero is sent, and the server decides.

The cobra tree never drifts from what's implemented. Step down it with
`--help` at each level:

    c1i --help
    c1i mcp --help
    c1i mcp servers get --help

Where to look in the docs — a product concept, an endpoint's shape, or a
runbook — is its own section, "Finding things in the docs" below. To drive a
discovered endpoint directly:

    c1i api --path /api/v1/access_reviews --paginate

A few wire conventions if you build a raw request: GET endpoints take
`page_size`/`page_token` as snake_case query params; POST search endpoints
take `pageSize`/`pageToken` (camelCase) in the body; response pagination is
always `nextPageToken`. List/search responses wrap items under `"list"` —
except the MCP admin endpoints, which use resource-named keys (`"tools"`,
`"profiles"`, `"bindings"`, `"classifiers"`, or `"templates"`). `--paginate`
unwraps whichever field it finds, but pass `--list-key <field>` to name it
yourself rather than hand-rolling the loop when auto-detection picks the wrong
array. GET and DELETE refuse a body by default; the few endpoints that need
`--allow-delete-body`. The UI's "campaign" is the API's access review — a
campaign ID from a URL is the access review `id` directly, and the UI's "access
profile" is the API's catalog: `c1i access-profiles list`, `/api/v1/catalogs`, whose
`RequestCatalog` schema is tagged `x-speakeasy-entity: Access_Profile` in the
spec. Search for both names.

## Finding things in the docs

`docs` commands need no credentials, so reach for them before — or without —
authenticating. Which one depends on what you are after:

- **A product concept, or how the product does something** ("access reviews",
  "just-in-time access"): `docs search "<terms>"`. It returns up to 10 hits as
  NDJSON — each a `title`, a `path`, a `content` preview, and a `url` (the
  public docs page a human can open). The `content` is a snippet of the page —
  usually short, though an API-reference hit can run to tens of thousands of
  characters — so read the authoritative full page with `docs page <path>` on
  the best hit (a leading slash or the full https://c1.ai/docs/ URL is also
  accepted). `docs search` is semantic with
  no true no-match (see "Global flags" above), so judge each hit by its snippet
  rather than trusting that a result exists.
- **Whether an API endpoint exists, or its request/response shape**: `docs
  endpoints --filter <text>` lists matching routes and has a real no-match — an
  empty result means nothing matched, not "the search gave up" — then `docs
  endpoint <path>` prints that route's full request/response schema (its
  top-level keys are HTTP methods; `--fields` projects it, e.g.
  `--fields get.summary`).
- **A step-by-step runbook**: `docs guide` lists the embedded guides with a
  one-line purpose, and `docs guide <name>` prints one. These are static
  content, no network call. The complete workflow index is:

  | Goal | Guide |
  |---|---|
  | Register, distribute, or test MCP tools | `register-mcp-server` → `assign-toolset-everyone` → `test-mcp-gateway` |
  | Retire an MCP server | `decommission-mcp-server` |
  | Create a manually managed app | `configure-new-app` |
  | Request or approve access | `request-access` → `inspect-and-approve-task` |
  | Configure delegated provisioning | `delegate-entitlement-provisioning` |
  | Promote an identity-provider app to a managed app | `promote-idp-app-to-managed` |
  | Run an access-review campaign or template | `run-access-review` |
  | Diagnose a failing connector sync | `diagnose-connector-sync` |
  | Set up cross-app access (XAA) | `configure-cross-app-access` |
  | Set up workload federation and trusts | `configure-workload-federation` |
- **The command tree this build accepts**: `docs map` prints an indented
  outline; `docs map --json` adds full paths, aliases, and counts for scripts or
  docs checks. No auth or network.
- **The raw OpenAPI spec**: `docs openapi` (cached 24h locally).

Rule of thumb: a product concept starts at `docs search` → `docs page`; a raw
`c1i api` call starts at `docs endpoints --filter` → `docs endpoint`.

## Reading output

- List commands emit NDJSON: one object per line. In rows c1i builds, a string field with nothing to report is `null`, never `""`, so `jq 'select(.field)'` selects only rows that have it. Some keys are present only when they apply: `type`, `app_id` and `app_entitlement_id` on `tasks`/`requests` rows (by task type), `outcome` on `tasks`/`requests` rows, `tool_count` and `last_called_at` on `mcp servers search` rows (only with `--tool-state` / `--include-last-called-at`, and `last_called_at` only once the server has been called), and `origin` on `users list` rows. With `--fields`/
  `C1I_FIELDS`, a row whose projection matches nothing is skipped entirely
  (never printed as `{}`), so the line count can be less than the underlying
  result count.
- Single-object reads emit pretty-printed JSON.
- Typed `get` commands unwrap the API envelope (since v0.6.0): they print the
  resource itself, so read `.id` — not `.app.id`, `.userView.user.id`, or any
  other wrapper key. Naming a wrapper key in `--fields` matches nothing and
  exits 2. Exceptions: `mcp servers get` identifies itself with `.connectorId`;
  `entitlements proxy-bindings get` has a directional composite identity:
  `.srcAppId`, `.srcAppEntitlementId`, `.dstAppId`, and
  `.dstAppEntitlementId`. `c1i api` still returns the API's envelope.
- Mutations print the API's response unwrapped the same way, as pretty JSON.
  **Read the id from the path the command documents; it is not always
  `.id`:** `.connectorId` (MCP servers), `.srcAppId` (proxy bindings),
  `.runId` (`role-mining trigger`), `.bulkActionId` (`findings bulk-*`),
  `.accessProfileId` (`role-mining access-profiles create`), `.taskId` and
  `.finding.id` (`findings create-task`), and `.id` for the delivery plus
  `.webhookId` for `webhooks test`. Deletes and other empty responses print
  `{}`: exit 0 is the success signal. `mcp tools approve` prints one compact
  line per tool. A task's `.state` (task actions, `requests create`) is the
  server's snapshot, not the outcome; re-read with `tasks get`.
- Mutation output is never projected: an explicit `--fields` on a mutation
  (even `--fields ""`) exits 2 before anything is sent, and `C1I_FIELDS` is
  ignored there. The raw reads `findings settings show`, `findings settings
  edge show`, `settings ai-governance tenant-defaults` and `mcp gateway call`
  refuse an explicit `--fields` too.
- Casing differs by mode: list rows are snake_case (`app_id`,
  `display_name`); single-object reads carry the API's own camelCase
  (`displayName`, `createdAt`). `--fields` matches either casing at any
  depth — `jq` doesn't, so check a row's actual keys before writing a filter.
- Values keep their real JSON types: booleans and numbers are never
  stringified, so `jq 'select(.enabled)'` and numeric comparisons behave.
- `--fields id,user.email` projects to just those dot-paths; `C1I_FIELDS`
  sets the same thing for the whole session. A spec that names no path (`,`,
  `"$A,$B"` with both unset) or has an empty segment (`id.`) exits 2 from any
  of the three channels, rather than printing full rows.

Choose the simplest output tool:

- Use `--fields` when you know the keys to keep. It has no external
  dependency, preserves NDJSON, accepts either snake_case or camelCase field
  names, and errors (exit `2`) if no requested key exists anywhere in the
  result.
- Pipe to `jq` only for conditional selection, reshaping, aggregation, sorting,
  or extracting a value from mutation output (use `jq -er`, which fails on a
  missing id or a `--dry-run` preview where `jq -r` exits 0 with `null`).

```sh
# Known keys: prefer c1i's built-in projection
c1i users list --fields id,email
# Conditional selection: jq is appropriate
APPS_WITH_USERS="$(c1i apps list | jq -c 'if .user_count > 0 then . else empty end')"

# Filtering apps: `apps list` has no filters; use `apps search`
c1i apps search --query okta --exclude-app-id <app-id> --policy-id <policy-id>

# Mutation output: never projected; extract the id with jq -er
APP_ID=$(c1i apps create --display-name "Example" | jq -er .id)
```

`user_count` is the platform's stored count, not a live tally: on some apps it differs from the number of `accounts list --app-id <app-id>` rows. To size a change, count those rows, not `user_count`.

## Exit codes

| Code | Meaning | Do this |
|---|---|---|
| 0 | success | — |
| 1 | generic / unclassified error | inspect the message; on a write, read `applied` (a `--wait` timeout or failed stdout write is `true`) before retrying |
| 2 | usage error (bad flags/args, an empty, `.` or `..` id, a blank flag or positional, a control character in a path flag (`-o/--output`, `--out-dir`, `--file`, `--*-file`), an invalid header name or a control character in a header value (`api --header`, MCP `--header-name`/`--header-value`/`--bearer-token`, and those fields plus `awsSigv4` `accessKeyId`/`sessionToken` in a `--*-config-file`, either JSON spelling), a control character in a `--config-field` key, a missing, malformed, `null` or wrong-shape `--*-file` input, a non-object element in an array `--*-file` or `--body-file`, a referenced id that exists but is soft-deleted or of the wrong kind (a deleted or wrong-type policy), a refused redirect (but `upgrade` and the `docs` commands exit `8`), or any API `4xx` other than `401`/`403`/`404`/`408`/`429`/`499`) | fix the invocation, don't retry as-is — but read the message first: an API `4xx` can also be a state rejection (e.g. `task is closed`) rather than a bad argument |
| 3 | not authenticated, or API `401`/`403` (including a `400`/`401`/`403` from the token endpoint; any other token-endpoint failure exits like an API response: `404` 4, `429` 5, `5xx` or a `200` that is not a token 6) | re-authenticate (`c1i auth login`); a scoped credential needs a role that covers the call |
| 4 | API `404`, or a referenced id that does not exist, whether the server or a c1i pre-check catches it | stop — the resource/path doesn't exist |
| 5 | API `429` | back off and retry |
| 6 | C1 failed: API `5xx`, a `200` with a body that isn't JSON or a usable token, a redirect chain that never settles, a stuck pagination cursor, a list page in a shape the command doesn't recognize, or a `get` answered `200` without the object it reads | retryable, except a stuck cursor or unrecognized page, which persists: the printed rows (if any) are real but incomplete. On a write, a `5xx` is `applied: "unknown"` (but `false` when the failure is at the token endpoint and nothing was sent yet) and a non-JSON `200` is `applied: true`: re-read before re-sending |
| 7 | `mcp gateway call` succeeded but the tool reported `isError: true` | inspect the printed result, not just the code |
| 8 | a system beyond C1, or the protocol layer, failed — including a gateway that is unreachable (DNS failure, refused connection) or `mcp servers test-connection` reporting an EXTERNAL server unreachable (the JSON is still printed) | retrying the same call usually repeats it. For a connector failure, check the server with `mcp servers get <connector-id> --app-id <id>` and `mcp servers test-connection`; the upstream may be unreachable or its credentials expired. For a protocol error, it's a version mismatch or a c1i bug — report it, don't work around it |

Exit 1 with `writing output:` means stdout failed (e.g. a full disk). On a
write the message is
`<METHOD> <path> succeeded but writing its response failed` and `applied` is
`true`: the change landed, so verify before retrying a mutation. A batch (`mcp tools approve`) can exit non-zero after applying
some ids; its summary's `notAttempted` lists the ids never sent.

Branch on the exit code, not stderr text. An empty id argument is a usage error
(`c1i users get ""` exits 2 without sending anything) — worth knowing if you
build ids programmatically, because it used to return the whole collection with
exit 0. An id of `.` or `..` (also with a `;` suffix, such as `..;x`, or
percent-encoded as `%2E`) is refused before sending too, since the API resolves
it to the parent or collection. That holds for every id argument (a flag ending
`-id`, `-ids`, `-uid` or `-uids`, or a positional documented `<…-id>`), whether
the id goes in a path or a body, and before any credential is loaded. An id of
`/` is never valid, and the exit
depends on the route: as the URL's final segment (`c1i users get /`) it escapes
to a path the API redirects to the collection, and the REST client refuses that
path change (exit 2); on a multi-segment route (`c1i apps owners /`) the request
targets `//` and the API answers 404 (exit 4). Either way you don't get a full
listing that looks like a successful read. It also refuses a same-path redirect
to an unrelated host or from `https` to `http`, since a followed redirect
carries your token. It does follow pure host canonicalization. `mcp gateway` is
guarded the same way: it is built on the same shared transport, which applies
the empty-path and redirect checks unconditionally.

`6` versus `8`: `6` means C1 itself failed, so waiting and retrying is sensible.
`8` means C1 answered and something past it did not — a connector is down, or
the protocol disagreed — so the same call will usually fail the same way. For
`mcp gateway call`, a JSON-RPC-level error (the call failed, not the tool) is
classified: `-32602` (invalid params, e.g. an unknown tool name) exits 2, since
that is caused by what you passed; `-32601`/`-32700`/`-32600` and an upstream
connector failure (code `0`) exit 8; a JSON-RPC error carrying no code at all,
and any other code, exit 1.

## Pagination

Most list commands auto-paginate to completion by default — one invocation
gets every page. Pass `--page-token` with a cursor to opt out and fetch a
single page manually; a blank value is refused (exit 2, before any request),
because supplying the flag at all turns auto-pagination off and an unset
variable would otherwise silently truncate the list. The exceptions send one
request. `role-mining suggestions|runs|custom-analysis list`, `findings
routing-rules|transformation-rules list`, `apps routing-rules list`,
`attribute-types list`, `bridges services`, `grants accounts` and `settings
ai-governance history`
each print a stderr `partial result` warning if the server reports more pages.
`mcp bindings by-tools` and `mcp toolsets requestable-connectors` are not
paginated.
A `--body-file` key that the command itself sets must be absent: any value,
including `null`, exits 2 before any request, listing every offending key. A
key set from a positional id is the one exception: it may be present when it
equals that positional exactly (a string), so `get` output round-trips into
`update --body-file`; any other value is refused naming the key, its value and
the positional. Paging keys have no exception. `findings search`, `findings audits search`, `findings
shadow-mcp-occurrences search`, `role-mining suggestions search` and
`role-mining suggestions users` refuse `pageToken`, `pageSize`, `page_token` and
`page_size`; use `--page-token` (one page) and `--page-size`. The positional-id
keys are: `findingId`/`finding_id` (`findings state|assignee`),
`suggestionId`/`suggestion_id` (`role-mining suggestions users`),
`templateId`/`template_id` (`mcp classifiers templates add-rule|instantiate`),
and `id` (`findings routing-rules|transformation-rules update`,
`automations|access-reviews|access-reviews templates|policies update`, `mcp classifiers update`,
`mcp classifiers tool-gates update`).
With `api --paginate`, a `pageToken` in the body (or `page_token` in `--query`)
is the first page's cursor, and later pages replace it; `pageSize` is sent as
given.
Don't write your own pagination loop.


`--page-size` is a request, not a promise: a page can come back with more
rows. Some endpoints won't return fewer than their server-side floor, but that
is not universal. `--page-size 0` uses the server's default of 25. A value
above the maximum is not an error; c1i clamps it. A negative page size or
limit rejects it before sending.

`--limit N` is the exact control: c1i enforces it client-side, whatever size
the server returns.

## Before you mutate

The `--body-file` owned-key rule (a key the command sets, or one a positional
owns, must be absent or equal to the argument) is described under Pagination;
it fires before any request, `--dry-run` included.

`--dry-run` (or `C1I_DRY_RUN`) previews a mutation without performing it:
one JSON object, `{"dryRun":true,"requests":[{"method","url","body"}]}`,
never with an `id`, plus a stderr notice. `upgrade --dry-run` prints the same
object on every non-error path without `--check` (never text; `--check` wins
over `--dry-run` and prints its bare report, `--fields` honoured), adding `.upgrade`: the
`upgrade --check` keys (always `current`, `latest`, `channel`,
`update_available`, `install_method`; `upgrade_command` only for a Homebrew,
`go install` or container install) plus `asset_url`, `sha256`, `destination` only when an update is due
on a standalone install. Its `requests` holds just the download `GET`
(manifest and signature were already fetched), `[]` when nothing downloads.
`--fields` is exit 2. Credential fields (names ending in token, secret,
password, signature, apikey and the like; header values; a URL's username (except `git`) and
password and secret-named parameters) print as `[redacted]`, a non-JSON body as
`"[redacted: not JSON]"`; the rest is as sent. It needs a configured URL (exit 2
without) and refuses an empty, `.` or `..` id (exit 2) as a real send does —
including one carried only in a request body (`--grant-policy-id`,
`--owner-id`, ...), so the preview cannot print a value a send would refuse. It
needs no credentials unless c1i must look something up (reads only; a preview
sends no write): the pre-send existence checks (user and policy ids, `policies
update`'s current type), which refuse as a real run does, a
task action's current policy step without `--policy-step-id`, `functions invoke`'s commit (`--commit-id` checked, or the published one
looked up), `functions update`'s current display name, a hosted MCP
server's catalog id (`mcp servers update-credentials` without `--catalog-id`), or the caller for
`requests create` without `--user-id`. `mcp gateway call` rejects it: c1i
cannot preview or suppress a tool's side effects, so inspect the tool first
and treat its call as live. `auth login` rejects it too.

A preview checks the input shape and runs the pre-reads a command documents.
An exit-0 preview does not prove an id exists: a command with no pre-read sends
no `GET`, so an unknown id previews at exit `0` and exits `4` for real. To
confirm an id exists, read it with the family's `get` or `list` command.

**An inherited `C1I_DRY_RUN=1` turns every write into a preview**, and
`jq -r .id` on a preview prints `null` and your script carries on. Extract ids
with `jq -er`, which fails instead.

Two things are irreversible in ways their `--help` doesn't make obvious:

- Calling an MCP gateway tool whose description marks it "requestable"
  files a real access-request task assigned to a human. It is not a dry
  read.
- `mcp servers delete` cascades: every toolset bound to that server, and
  each toolset's app entitlement, is soft-deleted with it. Anyone whose
  access came through one of those entitlements is affected.

A blank `--display-name` (empty, whitespace or invisible-only, such as a lone
U+200B) exits 2 on every create and update, before any request is sent.

One command sends more than one write: `entitlements create` POSTs a resource
type, a resource, then the entitlement, skipping the steps whose id you supply
via `--resource-type-id`/`--resource-id`. Its `--dry-run` previews all three in
`requests[]`, with `NEW_APP_RESOURCE_TYPE_ID`/`NEW_APP_RESOURCE_ID` literal in
the url and body. It prints the entitlement unwrapped (`jq -er .id`); on a
failure `applied` is `true` once any step was written, and `steps` lists them.
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

- A **repeatable** flag takes one value per occurrence; a comma is literal, not
  a separator. `--tool-id a,b` is one id, not two. `--config-field
  "region=us1,env=prod"` sets `region` to `us1,env=prod`, which the server may
  accept. Validate shell variables before supplying any repeatable flag:
  a comma is not a substitute for multiple values, and an empty value can
  silently remove a restriction. `--fields` IS comma-separated.
- Every named-value flag (`--state`, `--status`, `--type`, `--auth`,
  `--unbind-policy`, `--error-format`, `--method`, ...) ignores ASCII case,
  whitespace and `-` vs `_`, and takes the full API name too; `--help` lists
  the short names. An unknown or non-ASCII value exits 2 before any request.
  Where `--help` says "or any `PREFIX_*` value", a value with
  that prefix is sent upper-cased, `-` turned to `_`; `users list --status` and
  `entitlements create --resource-type` send any ASCII value that way. The
  only rewrites to a different enum are listed aliases:
  `MCP_TOOL_STATE_PENDING` → `MCP_TOOL_STATE_PENDING_REVIEW`, and
  `REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_BYPASS`/`_ENFORCE` → their
  `..._ENTITLEMENT_REQUEST_POLICY` names.
- A blank value (empty, whitespace or invisible-only) on any flag or
  positional exits 2 before any request, so an unset shell variable cannot
  drop a filter (`--state "$STATE"` no longer lists everything). Only flags
  where `""` means something take it: an update's `--description ""` is sent
  (clearing it where `--help` says so),
  `--fields ""`/`--error-format ""` override `C1I_FIELDS`/`C1I_ERROR_FORMAT`,
  and optional flags like `--comment`, `-o` or `--gateway-url` read it as not
  given. Never a filter. Env vars and `~/.c1i.yaml` go through their own
  parsers instead (an empty `C1I_FIELDS` is unset; `C1I_FIELDS=" "` exits 2).
- `mcp tools approve` takes one or more tool ids in a single invocation:
  `approve id1 id2 id3 --app-id A --connector-id C` approves a whole toolset in
  one process (one token, one TLS handshake). The API has no batch approve, so
  each id is still its own request, but don't loop the CLI per id — pass them
  all at once (pipe `mcp tools search --state pending --fields id | jq -r .id`).
  It prints one compact JSON line per applied tool (`.id`, `.state`), even for
  one id. A failed stdout write aborts the batch, and a partial failure exits
  with the first failure's code while earlier ids stay applied: under
  `--error-format json` check each per-id error's `applied` and the summary's
  `notAttempted`.
- `mcp prompts approve` and `mcp resources approve` work the same way; state
  is the only field those APIs let you change, so neither family has an
  `update`. `mcp resources list` includes removed resources; `search` hides
  them unless `--state removed`. `--include-grant-status` on either `search`
  adds `entitlement_active` and `grant_count`; without it those keys are
  absent, not false/0. A tenant without the prompts feature answers a
  well-formed prompts request with 403 `feature not enabled` (exit 3); the
  message names a feature, not a credential, so logging in again is unlikely
  to change it.
- Other `mcp` mutations print the response unwrapped (for ids see the
  Mutations bullet under Reading output). Deletes, `resync-tools` and `mcp
  bindings delete` print `{}`; capture ids with `jq -er`, which fails on a `--dry-run` preview.
  `update-credentials` without `--catalog-id` resolves it first and reports
  `Resolved catalog id: …` on stderr. `mcp servers register` reads the app
  first (unknown `4`, deleted `2`). `mcp servers register --wait
  [--wait-timeout 1m]` polls `mcp servers list` until the new server is listed
  (a list read right after register can omit it for a second or two; a timeout
  exits 1). A register response with no server id exits 6, with or without
  `--wait`.
- `connectors create` reads the app first (unknown `4`, deleted `2`) and
  refuses a `--body-file` that sets `appId` (the path owns it). It makes a
  connector with an empty config; set it with
  `connectors update <id> --app-id A --body-file <file> --update-mask config`,
  the file being `connectors get` output with `.config` edited and `expanded`,
  `appPath`, `capabilitiesPath`, `usersPath` deleted (else 400). Update ignored
  a displayName/description mask in the runs checked. `connectors delete` refuses a connector backing an MCP
  server (exit 2; use `mcp servers delete`, which also removes its toolsets).
  `connectors sync` only queues a sync; poll `connectors sync-status`, or pass
  `connectors sync --wait [--wait-timeout 15m]` to block until a sync started
  after the command ran reaches a terminal `.status`, then print the
  connector's whole `.status` object in place of the API's empty `{}` (a 200
  without `connectorView.connector` exits 6; a never-synced connector reports
  `SYNC_STATUS_UNSPECIFIED` with null timestamps, or omits `.status`, and is
  waited on either way). `SYNC_STATUS_ERROR`, a
  `.status.lastError`, or the timeout exits 1 with `applied: true`; the sync
  was still queued. A status that never advances `.status.completedAt` (an
  unconfigured connector stays `SYNC_STATUS_DONE` with null timestamps; a
  refused sync stays `RUNNING`) is reported at the timeout, not as a sync
  error -- it cannot be told from a sync that has not started yet, so
  `connectors get` (which shows `.status.lastError`) is where the reason is.
  `.status.errorKind` is not checked: live it was `"ERROR_KIND_UNSPECIFIED"`
  (the enum's zero value) on a clean DONE too, not `""`.
  Why did a sync fail: `connectors debuggable` (NDJSON: `appId`,
  `connectorId`, `syncStatus`) finds targets; then `sync-history <id>
  --app-id A [--runs N]` (runs + `.streak`; each `.lifecycleId`),
  `sync-errors` (`--top-k`), `sync-changes` (`--since` RFC 3339) and
  `sync-compare --left L --right R` (lifecycle ids from `sync-history`; the
  draft-test form is `--body-file`, not exercised live). All print the API
  object as returned; unknown well-formed id exits 4, malformed 2.
  `connectors schedule --cron X [--timezone Z] [--justification R]` or
  `--clear`. `connectors credentials rotate` prints `.clientSecret` once and
  leaves the previous credential valid for days; end it with `credentials
  revoke`. `credentials get` refuses a credential whose `appId`/`connectorId`
  differ from the flags (exit `4`): the server ignores both path segments.
- Promote a connector-discovered app to a managed one with `apps managed-state
  list|get|promote` (`--app-id` the connector app, `--resource-type-id` its
  resource type named `app`, from `apps resource-types list --app-id`;
  `get`/`promote` take the row's `resource_id` positionally). `promote` creates
  an app and nothing checks for an existing one of the same name, so run `apps
  list` first and, after any failure, inspect before retrying. Promoting an
  already-managed binding exits 0 and returns the existing app. After `apps
  delete` of the promoted app the binding's soft-delete lags a few seconds, and
  a promote in that window creates another app, so wait until `managed-state
  get` shows `deletedAt` (then `promote` exits 4). The binding later reappears
  unmanaged, after which a promote creates a new app. The managed app is
  `.state.managed.appId` (top-level `appId` is the connector). Without
  `--user-id` the new app's owners were the connector app's; `docs guide
  promote-idp-app-to-managed` has the timings and the full flow.
- `apps routing-rules list|get|create|update|delete|reorder` manage an app's
  entitlement routing rules (`--app-id` scope; `get`/`update`/`delete` take
  `<rule-id>`). The first enabled rule (by `priority`, 1 first) whose CEL
  `--condition` matches supplies the request settings (`--request-policy-id`,
  `--emergency-grant-policy-id` + `--emergency-grant-enabled`,
  `--max-grant-duration`, `--request-schema-id`). Enabled rules route real
  requests; `create` is disabled unless `--enabled`. `create --priority` is
  an insertion slot (omit to append). `reorder --rule-id` (repeatable) must
  name every active rule exactly once, else 400 (exit 2). `delete` is soft;
  repeating it exits 4. Cap was 10 rules per app (400, exit 2, "This app
  already has the maximum of 10 entitlement configuration rules"). `list` has
  no paging and lists nothing for an unknown app (exit 0); other commands
  exit 4 for an unknown app or rule, or an unknown policy or schema id;
  `create`/`update`/`reorder` exit 2 for a deleted app, dry-run included.
  `update` is read-merge-write (the API has no mask and clears omitted
  fields): only the flags given change, a concurrent change is overwritten,
  a deleted rule exits 2, and it cannot reorder or unset a policy/schema id.
- `apps usage-controls get <app-id>` shows the notify/revoke-on-non-use
  controls beside `hasUsageData`; never-set controls print only
  `hasUsageData`. c1i reads the app first, so an unknown app exits 4 (the
  endpoint itself answers 200) and a deleted one 2. No `update`: it answered 501 "feature is
  disabled" on the tenant tested.
- `apps resource-types get|create|update|delete <resource-type-id> --app-id`
  and `apps resources get|create|update|delete <resource-id> --app-id
  --resource-type-id` manage an app's manually managed types and resources;
  `list`/`search` read them. Deletes are soft: `get` still returns the object
  (`deletedAt` set), `list` and `search` stop returning it; both trail writes
  by a few seconds. A type delete did not delete its resources (one was
  still live ten minutes later, despite the API spec), so delete resources
  first. `resource-types create --resource-type` is required; only `CUSTOM`
  repeats per app (else 500, exit 6). `resource-types update` renames only.
  `resources create` exits 4 for an unknown type or `--owner-id` and 2 for a
  deleted type, before sending; its `--body-file` must not set `appId` or
  `appResourceTypeId`, which `--app-id` and `--resource-type-id` own (exit 2). `resources update --custom-description ""`
  clears it; a blank `--display-name`/`--description` exits 2 (the server
  ignores an empty one). Other fields (e.g. `annotations`, merged not
  replaced) go through `--body-file` + `--update-mask`. `resources search`
  filters: `--app-id`, `--resource-type-id`, `--resource-id`,
  `--exclude-resource-id`, `--trait-id`, `--exclude-trait-id`,
  `--owner-user-id`, `--unowned-only`; `resource-types search` takes
  `--app-id`, `--resource-type-id`, `--exclude-resource-type-id`, `--trait-id`,
  `--exclude-trait-id`. An unknown id matches nothing (exit 0).
  A type or resource id repeats across apps, so key search rows on `app_id`
  (and `resource_type_id`) plus `id`. `apps resources owners|add-owner|
  remove-owner|set-owners <resource-id>` behave like the app and entitlement
  owner verbs. The write verbs pre-read the app (unknown `4`, deleted `2`) and
  `set-owners` also pre-reads the resource (the API stores owners for an
  unknown one at 200); `owners` reads nothing first (a wrong `--app-id` lists
  zero rows). `owners` rows omit the response's `immutableUserIds`
  (owners the API says cannot be removed); read them with `c1i api`.
- Owner changes are applied asynchronously and can take several minutes to
  appear; pass --wait to block until they do. Grant provisioning is
  asynchronous too. A read immediately after a write can look like a no-op.
  Verify app owners with `c1i apps owners <app-id>`, not `apps get`'s
  `appOwners` field; an empty `appOwners` is not
  evidence an app has no owners. Use `apps set-owners --wait` when the write
  must converge before the next step; it waits until the owner set equals
  the request, removals included. `apps set-owners` prints the API's JSON
  response, `{}` (progress on stderr; a wait failure is `"applied": true`), and
  exits 4 before the write on an unknown
  user, which the API would store as an owner no user matches, and 2 on a
  deleted app. Entitlement
  owners work the same way:
  read with `c1i entitlements owners <entitlement-id> --app-id <app-id>`,
  write with `entitlements add-owner`/`remove-owner <entitlement-id>
  --app-id --user-id` (one user per call; a repeated `--user-id` exits 2, as on
  `apps add-owner|remove-owner` and `accounts set-owner`) or `entitlements set-owners <entitlement-id> --app-id
  --user-id ...`. `set-owners` replaces the list and needs at least one
  `--user-id`; an unknown user exits 4 before the write, because the API
  would silently drop it and could leave no owners. `remove-owner` exits 4
  both for an unknown user and for one who was never an owner. Pass `--wait`
  on `add-owner` (waits until the owner appears) or `set-owners` (waits until
  the owner set equals the request, removals included). A timeout exits 1,
  though the write was accepted — re-check before writing again. An unknown
  user, entitlement or app exits 4 on `add-owner`. `set-owners` sends a
  repeated `--user-id` once, since the API answers a duplicate with a 500.
  `entitlements owners` returns zero rows at exit 0 for a nonexistent
  entitlement or app, so an empty result can also mean "wrong id".
- An owner can also be an entitlement (its grantees own the object): on
  `apps`, `entitlements` and `apps resources`, `add-owner|remove-owner` take
  `--owner-entitlement-id` instead of `--user-id` (plus
  `--owner-entitlement-app-id` when the entitlement is in another app), and
  `owners --entitlements` lists them (`id` is the owning entitlement's). They
  use the v2 owner routes, role `primary` (the only one the API supports);
  an unknown entitlement or owned object exits 4, and `add-owner` refuses
  `--wait` in this form. `set-owners` with any `--owner-entitlement-id` also
  replaces user owners, so it needs `--user-id` for each user owner to keep or
  `--no-user-owners`, else exit 2. Its user part reached the v1 reads 1-2
  minutes late on an app and not within 8 minutes on a resource, so `--wait`
  polls `GET .../ownerids` plus the entitlement list and can time out (exit
  1, applied). Without `--owner-entitlement-id`, entitlement owners are left
  alone.
- `apps update` reads the app first: a malformed id exits `2`, an unknown
  app `4` and a soft-deleted one `2`, before anything is sent (`--dry-run`
  included).
- `apps update`/`apps create` `--grant-policy-id`, `--revoke-policy-id` and
  `--certify-policy-id` are checked by c1i (exists, not deleted, right
  `policyType`) because the API itself stores any id with exit `0`; raw `api`
  calls get no such check. An unknown policy is exit `4`; a deleted or
  wrong-type one is `2`. An empty id is exit `2`; unbind with
  `--unbind-policy <kind>`. An app user mapper set with
  `apps update --app-user-mapper-file` can be replaced but not removed.
- `--unbind-policy grant` (or `revoke`) leaves the app with no grant (or
  revoke) policy; the server accepts that with exit `0`.
- An app user mapper is stored whatever `--identity-matching` is set to.
- Binding a policy needs read access to policies: a 403 on the lookup exits
  `3` and nothing is sent.
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
- A `--wait-*` flag (`--wait-timeout`, `--wait-stable`, `--wait-min`) given without `--wait` exits `2` (an explicit `--wait=false` is accepted).
- `--wait-stable` counts consecutive identical reads (default `3`, minimum
  `2`). Two is not enough: a pause mid-change is indistinguishable from
  completion. Three is a heuristic, not a proof -- size it past the longest
  pause you have actually observed. The 5s poll interval is fixed, not a flag.
- `accounts list --unmapped-only` filters after each page is fetched, not
  server-side. With `--page-token` (which turns off auto-pagination) a page
  can come back empty while unmapped accounts exist further along.
- `accounts get <app-user-id> --app-id <id>` has no GET-by-id route to call:
  it runs the same `search/app_users` `list` does, filtered to the one id,
  and prints the app user unwrapped -- `profile` included, which `list` rows
  also now carry (`--fields profile.<key>`). Zero matches, or a result whose
  id doesn't actually equal the one asked for, exits `4` like any other
  not-found; more than one row claiming that id exits `6`.
- `functions invoke` is asynchronous: it prints `{"invocationId": ...}` and
  exits 0 before the function runs. Add `--wait` to block until it finishes.
  Any `--wait` failure exits `1` after the id was printed, with an error
  saying the invoke "was applied" -- that only means the run was started. A
  failed run is the error naming `ended in FUNCTION_INVOCATION_STATUS_ERROR`
  (or `CANCELLED`); a timeout says so and is not necessarily a failure. Its output and error are in `functions invocations get
  <invocation-id> --function-id <function-id>`. Without `--commit-id` it runs
  the published commit, and an unpublished function exits `2`.
- Code pushed with `functions create --file` or `functions update --file` is
  not published, so `functions invoke` keeps running the old commit until you
  pass the printed `.commit.id` to `functions update --publish-commit`. A
  `--update-mask` naming `displayName` with no value in the object exits `2`
  instead of resending the current name.
- `functions invocations list <function-id>` is newest first and a busy
  function can have tens of thousands of invocations: pass `--limit`.
- `functions usage` filters automations client-side by
  `callFunction.functionId`, same as `accounts list --unmapped-only` above.
  With `--page-token` a page can come back with zero rows while a matching
  automation exists on another page.
- A `--fields`/`C1I_FIELDS` projection that matches nothing scans until the
  command can prove no row contains that field, even when `--limit` is set.
  Correct the field name instead of treating a long-running command as an API
  failure.
- A task's `outcome` field is omitted while it's unspecified, not while the
  task is open — a task can be `TASK_STATE_OPEN` and already carry a real,
  non-UNSPECIFIED outcome (e.g. a provisioning failure mid-flow). Use
  `state`, not the presence of `outcome`, to tell whether a task is still
  pending.
- The `/api/v1/tasks/{id}/action/*` endpoints echo the task as it was *before*
  the action. Live: closing an open task returned `TASK_STATE_OPEN`, and
  restarting a closed one returned `TASK_STATE_CLOSED`. `tasks
  close`/`reassign` and every other task action print the task unwrapped
  (`jq -er .id`), so `.state` is that pre-action snapshot, not the outcome:
  re-read with `tasks get` before acting on it. A step c1i resolved is reported
  on stderr (`Resolved policy step: <id>`) only; if you call these actions
  through `api`, read the task back rather than trusting the response's
  `state`. The same holds for `restart`, `reset`, `skip-step`, `process` and
  `update-grant-duration`. `reassign` looks up each `--to-user-id` before
  sending: an unknown user exits 4, because the API stores it as the step's
  approver (`--dry-run` looks them up too, so a preview refuses what a real run
  does). A repeated id is sent once. Recover a step that already names a bad
  approver by re-running `reassign` with valid ids; a system owner can also
  `reset`/`restart` where the task offers them.
- `skip-step` refuses the current step when it is not an approval step (exit 2,
  nothing sent). Skipping a provision step abandons provisioning (the task can
  stay there with no grant or close); name it with `--policy-step-id` only if
  you intend that.
- Which actions a task accepts depends on its state, and the server refuses the
  rest with `action not permitted`. Read the task's own list first:
  `c1i api --path /api/v1/tasks/<task-id> --fields actions`. `restart`, `reset` and
  `skip-step` each rotate the current policy step (measured), so a
  `--policy-step-id` captured before one of them is stale and answers `this
  action is no longer available: the request has advanced to a new approval
  step` -- omit the flag to act on whatever step is current.
- `restart` re-runs the current approval step (one new history entry); `reset`
  restarts the whole policy (four, measured). Either can reopen a closed task,
  but whether it does depends on how the task closed: `reset` reopened
  CANCELLED, GRANTED and REVOKED closes but not a DENIED one -- exit 0, the
  task stays CLOSED with outcome DENIED. `restart` reopened a GRANTED close but
  answered `task is closed` (exit 2) on CANCELLED and DENIED ones (REVOKED
  untested). Neither is a general undo: re-read with `c1i tasks get` instead of
  trusting either exit. `process` changes nothing observable on a healthy task.
  `update-grant-duration` takes a whole-number Go (`1h`) or protobuf (`3600s`)
  duration (the server stored `0s` without complaint); it only works before
  provisioning, after which the server says
  `cannot update grant duration for a ticket in a provision step`; the value
  lands at `.type.grant.grantDuration` (there is no top-level `.grantDuration`).
- `tasks escalate <task-id>` moves a grant task onto its entitlement's
  emergency-grant policy (then `.emergencyAccess` is true); it is refused with
  `action not permitted` with no emergency-grant policy or when already
  escalated. The API describes `tasks retry-provisioning <task-id>` as
  re-driving a failed provision step; only its refusal was observed (`action
  not permitted` at an approval step). Both resolve the current step like
  `approve` and send the id that read returns, so a numeric task id works;
  with `--policy-step-id` nothing is read and the 27-character id is needed.
- The API describes `tasks update-request-data <task-id> --data-file <json>`
  as submitting the request form of a task at a form step (keys:
  `.policy.current.form.form.fields[].name`). Success was not observed: an
  object that does not fit gets `request data does not match the required
  schema (...)`, and a valid one on a self-requested form task (its `.actions`
  lacked `TASK_ACTION_TYPE_UPDATE_REQUEST_DATA`) got `action not permitted`;
  another step gets `current policy step is not a form instance`. Nothing is
  read first, so pass the 27-character task id.
- `tasks audits <task-id>` streams the task's audit events as NDJSON, raw,
  oldest first (`--newest-first` reverses); each row has `.eventType`,
  `.currentState`, `.created`, `.userId` and one event key such as `.comment`
  or `.stateChange`. `--comments-only`/`--exclude-comments` filter comments
  (both together exit 2). The task is read first, so an unknown id exits 4 (the
  audit endpoint alone returns an empty list) and a numeric id works. `approve-with-step-up` is not
  wrapped: it hands back a browser `redirectUrl`; use `c1i api`.
- `requests create action --action-id` (request action; `--description`,
  `--form-values-file`, `--group-only`) and `requests create resource-action
  --action-id` (connector resource-create, e.g. a group template;
  `--form-values-file`) create action tasks. The API has no route that lists
  these actions, so an id cannot be looked up. An unknown id exits 4 (`action
  not found`).
- `requests create grant --duration` likewise takes `24h` or `86400s`; the
  server refuses `0s` with `value must be greater than 0s`. The value lands at
  that same `.type.grant.grantDuration` path.
- `requests create grant|revoke --app-user-id` targets one application account
  instead of a C1 user; the account is looked up under `--app-id` first (an
  unknown one exits 4). Either `--user-id` or `--app-user-id` identifies the
  recipient; with both, the server requires the account to belong to the user
  (a mismatch is a 400).
- `requests create grant --wait` blocks until a new grant for the request
  appears in `grants list` and the set settles; the task is printed immediately and
  progress goes to stderr. `--wait-min` defaults to 1, `--wait-stable` to 3,
  `--wait-timeout` to 4m; a timeout exits 1 although the request was accepted.
  A request that still needs approval does not produce the grant until it is
  approved, so it waits until the timeout. The wait reads the matching set
  before sending and settles only on a grant that was not already there; a
  recipient who already holds the entitlement therefore waits to the timeout
  (the request may still be pending). `--dry-run` never polls.
- `grants add --app-id --entitlement-id --user-id ... [--wait]` grants a
  manually managed entitlement directly, **bypassing grant policy and
  approval**; prefer `requests create grant` unless that is what you want. One
  request carrying every user id; `--wait` blocks until each shows in `grants list`.
  `grants remove --app-id --entitlement-id --app-user-id` (remove-membership)
  refused manually managed entitlements of the custom and group resource types
  in testing with `entitlement is not an SSO application sign-in entitlement`,
  so remove those via `requests create revoke` (on a
  manual entitlement its task waits on a provisioning step; the grant stays
  until it is completed); its success path is untested. A non-empty
  `failedUsersErrorMap` in the response (users the server could not add) exits
  1 after printing it; `--wait` waits only for the others.
- `grants update-duration --deprovision-at <RFC3339>` / `grants remove-duration`
  (both `--app-id --entitlement-id --app-user-id`) set or clear when an existing
  grant is revoked; the server refuses a past time. `grants accounts --app-id
  --entitlement-id --user-id` shows which of a user's accounts hold the
  entitlement, and `entitlements grants <entitlement-id> --app-id` lists its
  grants with `expires_at` and `discovered_at`; both return zero rows at exit 0
  for an unknown id.
- `entitlements automation get|create|update|delete <entitlement-id> --app-id`
  works only on C1 app entitlements and acts on real users once it exists.
  Pick one condition: `--cel`, `--refs-file` (each ref looked up, unknown exits
  4, deleted 2) or `--none`; `create --body-file` for the full object. `get` exits 4 with
  no automation; delete is soft (`deletedAt` set), and `update` and
  `exclusions add|remove` still exit 0 on a deleted one, so check `deletedAt`.
  `exclusions
  list|add|remove <entitlement-id> --app-id [--user-id ...]` manages users the
  automation leaves alone.
- A user id on a write is looked up before anything is sent, so an unknown one
  exits 4 with no write (`--dry-run` refuses it too; the lookup needs user
  read, else exit 3): `accounts set-owner`, `apps add-owner|set-owners`,
  `entitlements add-owner|set-owners|create --owner-id`, `grants add --user-id`,
  `requests create grant|revoke --user-id`, `mcp servers register --user-id`, `tasks reassign --to-user-id`.
  `remove-owner` never pre-checks (the server decides; a `--dry-run` preview
  can pass an id the real run 404s). Only flag values are checked: user ids
  inside a `--*-file`/`--body-file`, or sent via `api`, go as-is. A
  `requests list --user-id` filter is not checked: an unknown one returns no
  rows. `grants list --user-id` is sent to the search API, whose unknown-user
  404 exits `4`.
- Entitlement ids are unique only within an app — some system-builtin
  entitlements reuse the same id across every app that has one. Always key
  on `(app_id, id)` together, never `id` alone.
- `POST /api/v1/apps/{app_id}/entitlements` requires `appResourceTypeId` and
  `appResourceId` even though its OpenAPI schema lists only `displayName` as
  required; omitting them 400s on the id regex. `entitlements create` handles
  this for you.
- `entitlements create` and `entitlements update` take a Go duration for
  `--duration-grant` (`8h`) as well as `3600s`, like every duration flag that
  goes to the API. `--emergency-grant-policy-id`
  takes a grant policy, checked client-side (the server checks only when the
  same call enables emergency grants). `--emergency-grant-enabled=true` needs
  `--emergency-grant-policy-id` in the same call even when one is already
  stored; to unbind and disable together pass `--emergency-grant-enabled=false`
  with `--unbind-policy emergency-grant`; `--unbind-policy emergency-grant` alone leaves an entitlement that
  has emergency grants enabled still enabled, with no policy.
- `mcp servers test-connection` returns `toolCount` as a JSON string, not a
  number. The `tool_count` in NDJSON list rows is a real number. It exits `8`
  when the server is unreachable (`reachable` is not `true`), after printing
  the same JSON, so check the exit code before reading `toolCount`.
  `--dry-run` previews the probe and sends nothing.
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
  Measured caveat: one run got `200 {}` (and a stored binding) from both a
  requestable and a visibility add on an unpublished catalog; the binding is
  not effective until publication, so publish first and verify with
  `access-profiles get` rather than relying on the refusal. The commands are
  `access-profiles create --published`, `visibility-entitlements add` and
  `requestable-entitlements add` (not the raw binding routes).
  `--enrollment-behavior unspecified` resets to the server default (measured:
  ENFORCE becomes BYPASS); `--unenrollment-behavior unspecified` resets to
  LEAVE_ACCESS_AS_IS, and the third flag's reset value was not measured.
- `access-profiles delete <access-profile-id>` is a soft delete: the catalog
  leaves `access-profiles list`, while `access-profiles get` still returns it at
  exit `0` with `deletedAt` set. So a `deleted_at` in an `access-profiles list`
  row is null in practice — don't read the null as "not deleted", check with a
  get.
- `service-principals delete` removes the principal (README: it also removes
  its credentials): `service-principals get` then exits `4`. `users get
  <sp-id>` still shows `"status": "DELETED"` and `deletedAt`.
  `service-principals credentials revoke` is soft: `credentials get` still
  returns the credential at exit `0` with no field marking it. Creates and
  updates print the object unwrapped (`jq -er .id`); `credentials create` adds
  `.clientSecret`, shown once, so never log it. Deletes, `revoke` and
  `bindings add|delete` print `{}`.
- `bridges` (tunnel bridges): `credentials create <bridge-id>` prints
  `.clientSecret` once, so never log it. `delete` of an unknown bridge, and
  `services` or `credentials list` on one, exit `0`; check with `bridges get`
  (exit `4`).
- Workload federation: `workload-federation providers` (alias `wf`)
  registers JWT issuers (`--well-known-provider spiffe` requires
  `--bundle-endpoint-url`; other kinds refuse it); `service-principals trusts`
  binds one to a principal with a CEL `--condition`, plus repeatable
  `--scoped-role`, `--allow-cidr` and `--passthrough-claim`. `<trust-id>` is
  the row's `client_id` or its part before `@`; get/update/delete/test-token
  need `--service-principal-id`. `trusts create` looks up `<sp-id>` and
  `--scoped-role` first (the API stores unknown ones): unknown exits `4`.
  Trust and provider deletes are soft (`get` still returns them, unmarked;
  list/search omit them); deleting a principal deletes its trusts.
  `test-cel --expression --claims|--claims-file` and `trusts test-token
  --subject-token-file [--source-ip]` print the API's answer, then exit `0`
  on a match/pass and `1` otherwise; `test-cel` exits `2` when the API reports
  an evaluation error.
- `automations update <automation-id> --enabled` succeeds on an automation
  with `isDraft: true`, and `isDraft` stays `true`. `--body-file` with
  `--update-mask triggers` replaces the published `triggers` and increments
  `currentVersion`; `isDraft` and `draftTriggers` are left as they were. An
  unknown camelCase mask path is a `400`, `invalid update_mask`; a snake_case
  one is a `400`, `FieldMask.paths contains invalid path`. A malformed id is a
  `400` (`value does not match regex pattern "^[a-zA-Z0-9]{27}$"`, exit `2`),
  not a `404`. `automations update` prints the automation unwrapped
  (`jq -er .id`) with `webhookCapabilityUrl`/`webhookHmacSecret` as top-level
  siblings; `automations delete` prints `{}`.
- `automations create` takes `--display-name`, `--description`, `--enabled`,
  `--steps-file` (array, sent as `automationSteps`) and `--triggers-file` (array,
  sent as `triggers`). `--body-file` sends the full request and excludes every
  other flag. It prints the automation unwrapped (`jq -er .id`).
  `automations execute <automation-id>` starts a run, even of a disabled
  automation, and prints `{"executionId"}`. `--context-file` is a JSON object
  sent as the run's context. With `--wait`, an end state of ERROR or TERMINATE,
  or a circuit-breaker pause, exits `1` with `applied: true`; so does a timeout.
  `automations executions get <execution-id>` reads one execution (`.state`);
  `executions terminate` prints `{}`, and the state becomes
  `AUTOMATION_EXECUTION_STATE_TERMINATE` shortly after. `automations
  circuit-breaker clear [--decision run|cancel]`, `circuit-breaker
  resolve-paused --decision run|cancel` and `cancel-pending`, each with optional
  `--reason`, print `{"bulkActionId"}` and run asynchronously. c1i cannot read a
  bulk action, so exit `0` means accepted. `cancel-pending` can be off per tenant:
  `400` `cancelling queued automation runs is not enabled for this tenant`
  (exit `2`).
- `attributes` manages tenant attribute VALUES (Compliance Framework, Risk
  Level), not user attribute types: that is `attribute-types`/`attribute-mappings`.
  `attributes types` lists types; `list --type`, `search --type` and
  `create --type` take a type name or id and look it up first (`--dry-run`
  too), so an unknown type exits `4` (the server returns an empty list for
  `list`/`search` and creates the value anyway on `create`); a name shared by
  several types exits `2`. Rows are `id`, `type_id`, `value`, `created_at`,
  `updated_at`, `deleted_at`. `delete <value-id>` is soft (`get` still returns
  it with `deleted_at`; repeat delete exits `0`); there is no update. The
  per-type routes (`/api/v1/attributes/compliance_frameworks`, `.../risk_levels`)
  have no commands; use `api`.
- `hooks list|get|create|update|delete` manage tool-call interception hooks.
  Flags build a function hook (`--event pre-tool-use|post-tool-use|pre-output`,
  `--function-id`, `--filter-cel`, `--priority`, `--enabled`,
  `--managed-by-guardrails`); a built-in pattern or JSON patch hook needs
  `--body-file` (update: plus `--update-mask`); the server 403s a JSON patch
  hook when its preview is off. Created disabled unless `--enabled`. The server
  stores any `--filter-cel` (an uncompilable one too) at exit `0`;
  `update --clear-filter` clears it. `--priority` is `0` to `1000`: create is
  server-validated, update is not, so c1i refuses an out-of-range value
  (exit `2`) before sending. `--function-id` and `--function-commit-id`
  are looked up first (unknown: exit `4`, deleted: `2`, `--dry-run` too; not
  for `--body-file`). `delete` is soft: `get` still returns the
  hook with no deleted marker, `list` omits it, repeat delete exits `0`.
  `webhooks search --query TEXT --webhook-id ID...` matches names, and the
  index lags writes (use `list`/`get` for current state).
- `users set-delegation <user-id> --delegate-user-id ID` (`--start-at`,
  `--expire-at` RFC3339) prints the binding; `--clear` removes it. The server
  accepted past times and self-delegation; `users get` shows `delegatedUserId`
  only once the start has passed. `users profile-types <user-id>` prints
  `{"profileTypes": [...]}`. `policies validate-cel <condition>` is the safe
  CEL compile check (markers + `valid`, exit 2 when invalid); `policies
  test-provision <cel>` prints `{value, type}`, but a CEL that does not compile
  there is a `500` (exit `6`), unchecked by c1i.
- `xaa resource-servers|scopes|access-profiles` manage cross-app access
  objects under `--app-id` (README "Cross-app access (XAA)"). `resource-servers
  create --audience --display-name` (https, normalized, immutable, unique per
  tenant), plus `--description`, repeatable `--resource-uri`,
  `--max-grant-lifetime`, `--signing-algorithm`, `--require-proof-of-possession`,
  `--disabled`; `modifyClaimsHook` only via `--body-file` (its `functionId`
  and `commitId` are looked up first; unknown exits 4). `scopes create
  --resource-server-id --scope-value --display-name`; approve with `scopes
  update --state enabled` (`--state removed` exits 2). Creates look up
  `--app-id`/`--resource-server-id` first (unknown exits 4, deleted 2) because
  the server stores either. Updates read the object to fill server-validated
  fields; an update of a deleted object exits 0 and applies. Deletes are soft and cascade from a resource server to its scopes,
  profiles and their entitlements within seconds. `resource-servers search
  --disabled=false` means enabled only; omit for both. `access-profiles
  scopes|add-scopes|remove-scopes <access-profile-id> --app-id` list and
  change a profile's scopes (repeatable `--scope-id`, looked up first: unknown
  exits 4, deleted exits 2 on add). `xaa client-audience-mappings
  list|search|get|create|update|delete|history` take `--resource-server-id`,
  not `--app-id`; the mapping's id is its client key (positional, may be a
  URL, sent in the query or body). `create --client-key --audience-client-id
  [--disabled]` first searches the tenant's resource servers: not found, or
  deleted, or created about a second earlier exits 4. A deleted key cannot be
  recreated (exit 2).
- `webhooks create` and `webhooks update` take the endpoint as
  `--destination-url` (`--url` is the global tenant flag), and it must be an
  absolute `https://` URL with a host (an IPv6 literal host is refused); a
  violation, or a blank `--display-name` (empty, whitespace or
  invisible-only), exits `2` before sending. `webhooks test <webhook-id>` only
  queues a delivery: it exits `0` with `"state": "WEBHOOK_STATE_PENDING"`
  whether or not the URL is reachable.
  Its `.id` is the delivery and `.webhookId` the webhook; `webhooks create`
  and `update` print the webhook (`jq -er .id`), `delete` prints `{}`.
  `webhooks delete` is soft: `webhooks get` still returns the webhook at exit
  `0` with `deletedAt` set, and `webhooks list` omits it. `webhooks update` on
  a deleted webhook exits `0` and changes it, and it stays deleted.
  `webhooks list` pages exceed `--page-size` (a size below 5 is raised to 5,
  plus one row), so use `--limit` for an exact count. `webhooks update` with
  no field flag exits `2`. `--callback-timeout` takes a whole-number Go
  (`10m`) or protobuf (`600s`) duration (no sign, no decimals) from `0` to
  `720h`: anything else exits `2` before sending, and over `720h` update returns
  `callback_timeout must be between 0 and 30 days`. A webhook has no
  signing secret.
- `attribute-mappings` map an app's profile keys onto C1.ai user fields (run
  `attribute-types list` first; type ids differ per tenant, so pass `--type`
  by display name). `create` takes `--key` for `app_user_profile` types, none
  for `app` types (Manager Email), and `--key` plus `--custom-key` for Custom;
  a wrong one exits `2` before anything is sent, an unknown `--type` exits `4`,
  and `--dry-run` makes the same type lookup. It prints the mapping unwrapped
  (`jq -er .id`). Values are copied verbatim, evaluation is asynchronous with no
  completion signal (re-read the user; `count` is not a readiness check), and a
  second create for the same app and type makes a second mapping. The server's
  PUT ignores `updateMask` and replaces fallbacks and profile types it is not
  sent, so `update` reads the mapping and re-sends them. `delete` is hard
  (`get` exits `4`). Precedence across apps mapping the same type was not
  tested.
- `apps delete <app-id>` is soft: the app is marked with `deletedAt`
  (`apps get` shows it), and `apps list` omits deleted apps.
- `entitlements delete <entitlement-id>` is soft: `entitlements get` still
  returns it, with `deletedAt` set.
- `automations delete <automation-id>` is soft: the API's own list drops the
  automation, but `automations list` reads the search index, which can keep
  returning it with no deleted marker; `automations get` still returns it at
  exit `0` with no field marking it deleted.
- `policies delete <policy-id>` is soft: the policy leaves `policies list` and
  the default `search`, but `policies get` still returns it at exit `0` with
  `deletedAt` set (`search --include-deleted` also finds it).
- `users list` with no `--origin` returns directory-origin users only, so
  local-origin users (service principals among them) and system users are
  missing. Pass `--origin all` for every user, or `--origin local` for
  local-origin users only; each row's `origin` says which it is, and that value
  is also accepted by `--origin`.

## Upgrading c1i

`c1i upgrade --check` prints one JSON object and changes nothing: `current`,
`latest`, `channel`, `update_available`, `install_method`
(`standalone|homebrew|go-install|container|system|windows`), and, for a
Homebrew, `go install`, or container install, `upgrade_command`. `upgrade`
exits 0 without replacing anything for a source build, which prints the
channel's current release, or a non-standalone install, which prints that
command (Windows and system installs get a hint but no `upgrade_command`). Without a terminal it needs `-y`/`--yes`. `stable` is the
default channel; `latest` and `preview` are separate channels. `--dry-run`
fetches and verifies the release (index, manifest, signature, TUF root) and
checks the install directory is writable, then prints a JSON preview (see "Before you mutate"); it takes no lock, doesn't prompt, and installs nothing.

Exit codes treat the distribution center as a system beyond C1: a dist `404`,
`429`, or `5xx` exits 4, 5, or 6, but any other
dist `4xx`, a refused redirect, a redirect loop, a non-JSON `200`, or a TUF, signature, or
checksum failure exits 8.

## Carry forward

Record tenant-specific facts you learn this session — app/connector ids,
which MCP servers are EXTERNAL vs HOSTED, the tenant URL — in your own
memory or a local `AGENTS.md`. Don't re-record c1i mechanics (exit codes,
output format, flag names); those ship with the binary.
