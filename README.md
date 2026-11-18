# c1i

**C1 Interface** — and it looks like `cli`. Get it?

A command-line interface for the [C1.ai](https://www.c1.ai/) API built
for AI agents and scripts. Machine-readable output (NDJSON/JSON), explicit exit
codes, built-in API docs, and auto-pagination make it reliable to automate.
For a human-friendly CLI, see [cone](https://github.com/ConductorOne/cone).

## Contents

- [Installation](#installation)
- [Start Here](#start-here)
- [Quick Start](#quick-start)
- [Authentication](#authentication)
- [Configuration](#configuration)
- [Output Conventions](#output-conventions)
- [Command Reference](#command-reference)
- [Shell Completion](#shell-completion)
- [Version](#version)
- [Updating the binary](#updating-the-binary-c1i-upgrade)
- [Uninstalling](#uninstalling)
- [License](#license)

## Installation

Download signed macOS, Linux, and Windows releases with checksums, provenance, and SBOM attestations from the [official distribution center](https://dist.conductorone.com/ConductorOne/c1i).

```sh
# Homebrew
brew install conductorone/baton/c1i

# Go
go install github.com/ConductorOne/c1i@latest

# Container (<version> is the release number without the leading "v")
docker pull public.ecr.aws/conductorone/c1i:<version>
```

A standalone binary updates itself with `c1i upgrade`; see [Updating the binary](#updating-the-binary-c1i-upgrade).

## Start Here

Use this workflow for agent-driven and scripted operations:

1. Run `c1i docs agents` before operating on a tenant. It explains tenant
   selection, output and exit-code contracts, mutation safety, and raw API use;
   it needs no authentication.
2. Pass `--url https://<tenant>` on every C1 command, then authenticate and run
   `c1i auth whoami` before doing anything else.
3. Run every intended C1 REST write with `--dry-run` first. Gateway tool calls
   reject it and are always live.
4. Prefer a first-class command. Use `c1i api` only when no command covers the
   public REST endpoint.

## Quick Start

```sh
# Pass --url explicitly on every C1 call to avoid falling back to another tenant.
c1i auth login --url https://example.conductor.one

# Verify the authenticated identity and tenant.
c1i auth whoami --url https://example.conductor.one

# List users.
c1i users list --url https://example.conductor.one

# Explore the API — no credentials needed
c1i docs search "access reviews"
c1i docs endpoints --filter task
```

> **Upgrading from 0.9.x?** 0.10.1 changes mutation output (unwrapped JSON, like
> `get`) and several exit codes. Read "Upgrading from 0.9.2" in
> [CHANGELOG.md](CHANGELOG.md) before updating scripts.
>
> **Upgrading from 0.10.1?** Read "Upgrading from 0.10.1" there too: `.`/`..`
> ids exit `2` everywhere, unknown referenced ids exit `4`, error text masks
> secrets, and `tasks skip-step` refuses a provision step.

## Authentication

```sh
# Browser-based login (OAuth device flow)
c1i auth login

# Limit the browser-login credential to chosen roles
c1i auth login --choose-roles                  # pick from a menu after approving
c1i auth login --scoped-role basic-user        # repeatable; a role name or id

# Or store credentials directly
c1i auth login --client-id <id> --client-secret <secret>

# Check credential status (also reports the storage backend)
c1i auth status

# Show the authenticated principal (`principleId` as the API spells it, `userId`, role/permission/feature counts, and
# display name + email when a best-effort secondary lookup succeeds) plus the resolved
# tenant: "tenant" (base URL) and "tenantSource" (flag/env/config)
c1i auth whoami
# --verbose swaps the summary for the raw introspect payload (full roles/permissions/
# features arrays, but no display name or email) -- a different projection, not a superset
c1i auth whoami --verbose

# Machine-readable "which tenant am I about to write to?" — the pre-write check
c1i auth whoami --url https://example.conductor.one --fields tenant

# Mint a short-lived bearer token for driving raw API calls yourself
c1i auth token            # add --json for token type and absolute expiry (RFC3339)

# Remove stored credentials (--dry-run is rejected: the delete cannot be previewed)
c1i auth logout
```

Browser login creates a personal client credential with all of your roles. To
limit it, pass `--choose-roles` (a menu after you approve in the browser; needs
a terminal) or `--scoped-role <role>` (repeatable). The two can't be combined,
and neither applies to `--client-id`/`--client-secret` (exit 2). When no
credential is stored for the tenant, a terminal login asks which you want:
Enter keeps all of your roles, and `--choose-roles=false` skips the question.

- **Which roles.** A scoped credential keeps only the overlap between its roles
  and your own access. The menu and role names offer the roles you hold, Basic
  User, and Read-Only Administrator (a read-only view of your access); holders
  of Super Administrator or Read-Only Administrator see every role. In the
  menu, enter numbers or `0` for all of your roles; a blank answer asks again.
- **Names and IDs.** A role ID is 27 letters and digits and goes to C1 as
  given; an unknown one exits 4. Anything else is a name: case is ignored, and
  spaces, underscores and hyphens are interchangeable. A name that matches no
  offered role, or more than one, exits 2 before your credential is created,
  listing the choices.
- **Checks.** Login verifies the new credential before storing it. If C1
  scoped it wider than asked (exit 6), a scoped credential has no access
  (exit 2), verification or storing fails, or you press Ctrl-C, login deletes
  it. Scoped credentials are named `Created by c1i (<role or count>)`.
- **The temporary credential.** The device-flow token can only create a
  credential, so reading roles uses a short-lived one with all of your roles,
  deleted before yours is created. If it isn't deleted, login names it. If
  yours was stored, login exits 1; otherwise it exits with the login's own
  failure code and stores nothing.
- A browser login that replaces a stored credential names it; it stays active
  in C1.ai.

`c1i auth token` prints just the access token, newline-terminated, so it
composes into `curl -H "Authorization: Bearer $(c1i auth token)" ...`. `auth
token` and `mcp gateway` mint a new token per invocation and never write it to
disk.

### Credential sources

`c1i` reads credentials from the first source that has them, in this order:

1. **Environment variables** — set `C1I_CLIENT_ID` and `C1I_CLIENT_SECRET`
   (alongside `C1I_URL`) for non-interactive / CI use. Both must be set;
   setting only one exits `2` (`auth login` and `auth logout` still run and
   warn on stderr).
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
the active credentials. If the config directory resolves to a relative path,
from a relative `HOME`, `%AppData%` or `XDG_CONFIG_HOME`, the file fallback
fails with an error and the token cache below skips its file, rather than
writing under the working directory.

### Token cache

To avoid minting a fresh OAuth token on every invocation — each mint writes a
`client_credentials` event to the tenant's audit log — `c1i` caches the access
token and reuses it until it nears expiry. It uses the OS keyring when
available. On Unix-like hosts without a usable keyring, it instead uses a
hardened `0600` file under the config directory (`~/.config/c1i/tokens/` on
Linux). A cached token the server rejects (clock skew, or a revoked credential)
is dropped and re-minted once automatically. Each file-cache write also deletes
the other token files there that c1i would refuse to load (expired or expiring
within a minute, corrupt or unreadable, or open to group or other users) and
temp files over a minute old.
The token is strictly shorter-lived than the client secret already stored
beside it. Set `C1I_NO_TOKEN_CACHE=1` (or `true`) to disable
caching and mint per invocation; `0` and `false` leave it on, and any other
value exits `2`.

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
domain, so a typo like `example.conductor.on` is accepted here and surfaces
later as an authentication failure.

A bare name (`--url example`) is rejected: with more than one tenant domain in
use it is ambiguous, and silently expanding it to `example.conductor.one` would
point an EU tenant at the wrong region. The error names where the value came
from, which matters when it is a stale entry in `~/.c1i.yaml` rather than
something you just typed. A single-label host is fine as long as it arrives as a
URL (`https://intranet-host`), such as a name only your resolver knows.

Set it via (in order of precedence):

1. `--url` flag
2. `C1I_URL` environment variable
3. `~/.c1i.yaml` config file:

   ```yaml
   url: https://example.conductor.one
   ```

   A terminal `c1i auth login` given the URL another way offers to save it
   there. If the file isn't a valid YAML mapping, login warns and leaves it
   unchanged. With a relative `HOME`, c1i neither reads nor writes it, so a
   `.c1i.yaml` in the working directory is never used.

These are equivalent:
- `--url https://example.conductor.one`
- `--url example.conductor.one`
- `--url EXAMPLE.CONDUCTOR.ONE` (the host is lower-cased)
- `--url https://example.conductor.one:443` (the default https port is dropped; any other port is kept)

A path, query or fragment in `--url` is ignored: requests go to the API root.

**Every command prints which tenant it's about to use when the URL came from
`~/.c1i.yaml`.** Nothing on the command line names the config file, so a
stale entry there sends a command to the wrong tenant with no visible sign
otherwise. `--url` and `C1I_URL` don't print this warning: both are explicit
choices made for that invocation, and warning on every normal use of
`C1I_URL` would just train you to stop reading it. The warning goes to
stderr, once per invocation (never once per page of a paginated list):

```
Warning: no --url flag given; targeting https://example.conductor.one (from ~/.c1i.yaml)
```

For credential storage, see [Credential sources](#credential-sources) below.

### Global flags

These flags apply to every command. A flag on the command line overrides its
environment variable.

| Flag | Env var | Default | Meaning |
|------|---------|---------|---------|
| `--url` | `C1I_URL` | none (then `~/.c1i.yaml`) | C1 tenant URL, `https` required; see [Configuration](#configuration) |
| `--fields` | `C1I_FIELDS` | none | comma-separated JSON field projection of read output; see [Field selection](#field-selection) |
| `--max-retries` | `C1I_MAX_RETRIES` | `4` | retries for transient API failures; `0` disables; see [Retries](#retries) |
| `--error-format` | `C1I_ERROR_FORMAT` | `text` | `text` or `json` errors; see [Errors & exit codes](#errors--exit-codes) |
| `--dry-run` | `C1I_DRY_RUN` | off | preview a C1 REST mutation without sending it; see [Dry run](#dry-run) |
| `--debug` | `C1I_DEBUG` | off | trace API HTTP requests to stderr; see [Debug tracing](#debug-tracing) |

### Retries

Transient API failures are retried automatically with exponential backoff and
jitter, honoring a `Retry-After` header when the server sends one. This keeps
long auto-paginated pulls from failing on a single rate-limit blip. What gets
retried depends on the request, to avoid duplicating side effects:

- **`429 Too Many Requests`** — retried for every command (the request is
  rejected before the server processes it, so a retry is always safe).
- **Transient `5xx` (500, 502, 503, 504) and network errors** — retried only for
  idempotent reads and updates (GET/PUT/DELETE) and for the read-only search and
  list `POST`s behind list commands (`/api/v1/search/*`, any `/api/v1/` path
  ending in `/search` or `/list`, `/api/v1/systemlog/events` for `export
  events`, and `role-mining suggestions users`). Other `POST` mutations
  (e.g. `requests create`, `tasks approve`) are **not** retried on these, since
  the server may have already applied the change before the failure.
  Non-transient 5xx (501 Not Implemented, 505, 511) are never retried.

Control the retry budget (attempts *after* the first try) via, in order of
precedence:

1. `--max-retries N` flag (any command that reaches the C1 API)
2. `C1I_MAX_RETRIES` environment variable
3. Default: `4`

Set `--max-retries 0` to disable retries entirely; a negative value, from the
flag, `C1I_MAX_RETRIES` or `max_retries` in `~/.c1i.yaml`, exits `2`. Non-retryable responses
(4xx other than 429, and 501/505) fail immediately.

A non-empty `C1I_DEBUG`, `C1I_DRY_RUN`, `C1I_MAX_RETRIES` or
`C1I_ERROR_FORMAT` value that doesn't parse exits `2` naming the variable
before any request; the flag given on the command line overrides the variable
entirely. The same keys in `~/.c1i.yaml` (`dry_run`, `debug`, `max_retries`,
`error_format`) are checked the same way, and the error names the file and key.
A `~/.c1i.yaml` that exists but can't be read or parsed (invalid YAML, not a
mapping, a directory, no read permission) exits `2` naming the file, even with
`--url`; a missing file is fine, and completion, `docs`, `help` and `version` ignore a
broken one.

This covers the token mint too: minting or refreshing the OAuth2 bearer
c1i authenticates with is itself a request, subject to the same 429/5xx/network retry
and the same `--max-retries` budget. A `400`/`401`/`403` from the token endpoint
means the credentials were rejected (exit 3, not retried); a persistent `429`
exits 5 and a persistent `5xx` exits 6. Against a hung token host the
worst case is about `(--max-retries + 1) × 30s` plus backoff.

Every request also gets a fixed timeout, per attempt (a retried request
gets a fresh budget, not a shrinking share of one deadline): 10 minutes
for a REST or MCP gateway request, 30 seconds for `auth login`'s
device-flow requests and the OAuth2 token mint/refresh above — tighter
because those are fast request/response exchanges, not the kind of call
that legitimately runs long. Neither is configurable.

### Dry run

`--dry-run` (or `C1I_DRY_RUN=1`) previews a C1 REST mutation and returns
without sending it. The preview is one JSON object on stdout, the requests in
send order with the full URL and the body as sent:

```sh
$ c1i requests create grant --app-id A1 --entitlement-id E1 --user-id U1 --dry-run
{
  "dryRun": true,
  "requests": [
    {
      "method": "POST",
      "url": "https://tenant.example.invalid/api/v1/task/grant",
      "body": {
        "appEntitlementId": "E1",
        "appId": "A1",
        "identityUserId": "U1"
      }
    }
  ]
}
```

A REST preview has only the keys `dryRun` and `requests` (one entry per
request: several for `mcp tools approve` with several ids, up to three for
`entitlements create`); it never has an `id`, and `body` is omitted when the
request has none. A stderr notice, `dry run: nothing was sent`, accompanies it.

`upgrade --dry-run` prints the same object plus an `upgrade` key, on every
non-error path without `--check` (`--check` wins over `--dry-run` and prints its
bare report, with no `dryRun` key and no notice; `--fields` is honoured): an update due, already current, newer than the
channel, a source build, or a Homebrew, `go install` or container install. It
is never text, and its stderr notice is `dry run: nothing was installed`.
`requests` lists only what a real run would send that the preview did not (the
asset download, one `GET`); it is `[]` when nothing would be downloaded. The
manifest and signature are fetched during the preview itself. `.upgrade` holds
the `upgrade --check` report: always `current`, `latest`, `channel`,
`update_available` and `install_method`; `upgrade_command` only for a Homebrew,
`go install` or container install; and `asset_url`, `sha256` and `destination`
(the binary it would replace) only when `update_available` is true on a
standalone install. `--fields` is a usage error on a preview.

Every preview, `.upgrade` included, prints credential fields as `[redacted]`: fields whose name ends
in `token`, `secret`, `password`, `signature`, `apikey` and the like (a
best-effort suffix rule), header values, a URL's username (except `git`) and password, and
secret-named URL parameters. Everything else is shown as sent, and a body that
is not JSON prints as `"[redacted: not JSON]"`. A 0.9.2 preview showed them as
typed.

It applies to every REST write command and to non-GET `api` calls, and never
sends that mutation. `mcp gateway call` rejects both `--dry-run` and
`C1I_DRY_RUN`: c1i cannot preview or suppress a gateway tool's side effects, so
inspect the tool and treat its invocation as live. `auth login` and
`auth logout` reject both too: c1i cannot preview or suppress storing or
deleting a credential, and a previewed logout used to delete it.

A preview resolves the URL first, so it needs one configured (exit `2`
without), and it applies the same id guard as a real send: an empty, `.` or
`..` id exits `2`. An explicit `--fields` exits `2` too, since a preview is
never projected. A preview needs no credentials, except where c1i must look
something up so the previewed body is exact:

- `tasks approve`, `deny`, `restart`, `skip-step` and `reassign` without
  `--policy-step-id` read the task to resolve its current policy step;
- `mcp servers update-credentials` for a hosted server without `--catalog-id`
  reads the server to resolve its catalog id;
- `requests create grant`/`revoke` without `--user-id` resolve the caller;
- the pre-send existence checks (a `--user-id`, `--owner-id` or `--to-user-id`
  user on a write (`remove-owner` excepted), a policy id on `apps` and `entitlements` writes, the policy type
  `policies update` looks up) run on a preview too, so it refuses what a real
  run refuses and a preview needs credentials with user (or policy) read; they
  are reads, and the preview still sends no write. The one exception is
  `remove-owner`, which never pre-checks: the server decides, so a preview can
  pass an id the real run 404s;
- `upgrade` fetches and verifies the release (see [Updating the binary](#updating-the-binary-c1i-upgrade)).

**An inherited `C1I_DRY_RUN=1` turns every write in a script into a
preview.** The preview has no `id`, so `jq -r .id` prints `null` and the script
carries on with it. Extract ids with `jq -er`, which fails on a preview:

```sh
APP_ID=$(c1i apps create --display-name "Example" | jq -er .id) || exit 1
```

### Debug tracing

`--debug` (or `C1I_DEBUG=1`) traces each API HTTP request to stderr — method,
URL, response status, and elapsed time, including every retry attempt. Headers
and bodies are never logged, and a URL's username (except `git`) and password, and the value of every
secret-named parameter (names ending in `token`, including pagination cursors such as `page_token`, `api_key`, ...) print as `[redacted]`, as in
`--dry-run` previews, so credentials don't leak. Output goes to stderr,
so it won't corrupt piped JSON on stdout:

```sh
$ c1i apps list --debug 2>trace.log
$ cat trace.log
> GET https://example.conductor.one/api/v1/apps
< GET /api/v1/apps 200 OK (142ms)
```

## Output Conventions

- **List commands** (`users list`, `apps list`, etc.) output NDJSON (one JSON object per line). In rows c1i builds, a string field with nothing to report is `null`, never `""`, so `jq 'select(.field)'` selects only rows that have it. Some keys are present only when they apply: `type`, `app_id` and `app_entitlement_id` on `tasks`/`requests` rows (by task type), `outcome` on `tasks`/`requests` rows, `tool_count` and `last_called_at` on `mcp servers search` rows (only with `--tool-state` / `--include-last-called-at`, and `last_called_at` only once the server has been called), and `origin` on `users list` rows.
- **Mutations** (create, update, delete, owner changes, task actions,
  bindings) print the API's response as pretty JSON, unwrapped the same way the
  matching `get` unwraps it; every other envelope key, `expanded` included,
  stays as a top-level sibling. **The id path varies by command:** most use
  `.id`, but an MCP server's is `.connectorId`, a proxy binding's `.srcAppId`,
  `role-mining trigger` prints `.runId`, `findings bulk-*` `.bulkActionId`,
  `role-mining access-profiles create` `.accessProfileId`,
  `findings create-task` `.taskId` and `.finding.id`, and `webhooks test`
  `.id` (the delivery) and `.webhookId`. Extract an id with `jq -er`, which
  fails rather than printing `null`. Deletes and other empty responses print
  `{}`; exit `0` is the success signal. A task's `.state` (`tasks` actions,
  `requests create`) is the server's snapshot, not the outcome: re-read with
  `tasks get`. `mcp tools|prompts|resources approve` print one compact JSON line per id.
- **`api`** outputs pretty-printed JSON. With `--paginate`, outputs NDJSON (one list item per line). A `200` whose body is not JSON is an error (exit `6`), not a silent pass-through — a `--path` that escapes the API prefix can reach the web app and return HTML. An empty body still succeeds, since some endpoints answer a write with nothing.
- **`docs`** commands output NDJSON (`search`, `endpoints`), pretty JSON (`endpoint`), YAML (`openapi`), or plain text (`page`).
- List commands auto-paginate by default, except the single-request ones the
  agent guide names under Pagination (the `role-mining` and `findings …-rules`
  lists warn on stderr when a result is partial). Pass `--page-token` with a
  non-empty cursor to fetch a single page manually; supplying the flag at all
  disables auto-pagination, so a blank value is a usage error (exit 2) rather
  than an empty filter.
- **Stuck cursor.** A page holding only rows already printed is skipped (apart
  from any rows without an id); a page with any new row is printed whole. If the
  server's cursor stops advancing (a repeated `nextPageToken`, or two or more
  pages in a row, at least 25 rows in all, holding only rows already seen), or
  it sends 100 empty pages in a row (`export events` excepted), the command
  keeps the rows already printed, warns on stderr that results are INCOMPLETE
  naming the route, and exits `6`; every auto-paginating list command stops on a stuck cursor. A page whose
  rows all lack the id the command reads is an unrecognized response (exit `6`);
  commands keyed on several fields, such as `grants list` and
  `entitlements list`, print such rows instead.
- `--page-size` **requests** a per-call batch size (max 100; `mcp tools
  history` and `mcp bindings history` allow 200). Most list commands default it
  to 50 (see `--help`); `--page-size 0` asks for the server's default of 25,
  not "none". It is not a guarantee: a page can contain more rows than you asked
  for, by an amount that varies per endpoint and per size, and a positive value
  below 5 usually returns 5 (`policies list` floors at 6; `mcp servers catalog
  list` has no floor). A value over the max is clamped by c1i rather than
  rejected. A negative `--page-size` or `--limit` is a usage error (exit 2),
  rejected before any request. Use `--limit N` for an exact total: it is
  enforced client-side, so it holds even when a page overshoots, and it stops
  auto-pagination once reached.

### Repeatable flags

A flag documented as **repeatable** takes one value per occurrence, and a comma
is a literal character rather than a separator:

```sh
c1i mcp bindings create --app-id A --connector-id C --toolset-id T \
  --tool-id tool-a --tool-id tool-b     # two tools
```

`--tool-id tool-a,tool-b` is one id containing a comma, not two ids. This is
easy to miss on `--config-field`, where `--config-field "region=us1,env=prod"`
sets `region` to `us1,env=prod` and the server may accept it.

An empty occurrence is a usage error (exit 2), rejected before any request, so
an unset shell variable cannot silently shorten a list or remove a credential
restriction. Contrast `--fields`, which *is* comma-separated.

### Blank values

A blank value (empty, whitespace or invisible-only) on any flag or positional
is a usage error (exit 2), rejected before any request, so `--state "$STATE"`
with `STATE` unset cannot silently list everything. A few flags take an exact
`""` because it means something: an update's `--description ""` is sent as
given, with the effect that command's `--help` documents (a clear on `apps`,
a restore-of-create-time-value on `entitlements`); `mcp servers update
--tool-prefix`/`--data-sensitivity` also send `""`, while the `access-profiles`
behavior flags send `""` and the server rejects it as an invalid enum (omit
them); `--fields ""` and `--error-format ""` override `C1I_FIELDS`
and `C1I_ERROR_FORMAT`; and optional flags such as `--comment`, `-o` or
`--gateway-url` read it as not given (a create's `--description ""` is sent
by `access-profiles create` and `webhooks create`, omitted by the others). Whitespace-only
is refused on those too. Filters never take an empty value. Environment
variables and `~/.c1i.yaml` are not covered by this rule.

### Enum flags

Every flag that takes a named value (`--state`, `--status`, `--type`,
`--policy-type`, `--sort`, `--auth`, `--unbind-policy`, `--error-format`,
`--method`, ...) parses it the same way: ASCII case and surrounding
whitespace are ignored, `-` and `_` are the same, and the full API name is
accepted in any case (`--state pending-review`, `--state
mcp_tool_state_approved`). Its `--help` lists the short names. An unknown or
non-ASCII value is a usage error (exit 2), rejected before any request. A blank
one is too, except the few that take an exact `""` (see [Blank values](#blank-values)).

Where `--help` says "or any `PREFIX_*` value", a value carrying that prefix
is sent upper-cased with `-` changed to `_`, so an enum newer than c1i still
works. Two flags whose API enums have no prefix, `users list --status` and
`entitlements create --resource-type`, send any ASCII value that way. The
only values rewritten to a different enum are these listed aliases:
`MCP_TOOL_STATE_PENDING` is sent as `MCP_TOOL_STATE_PENDING_REVIEW`, and `REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_BYPASS`/`_ENFORCE` as their `..._ENTITLEMENT_REQUEST_POLICY` names. No other prefixed value is ever remapped.

### Display names

A blank `--display-name` (empty, whitespace or invisible-only, such as a lone
U+200B) exits 2 on every create and update, before any request is sent.

### Durations

Every duration flag (`--duration-grant`, `requests create grant --duration`,
`tasks update-grant-duration --duration`, `--expires`, `--callback-timeout`, and
the `settings ... update` lifetimes, intervals and timeouts)
takes a whole-number Go (`8h`, `1h30m`) or protobuf (`3600s`) duration and sends
it in seconds. There is no sign, no decimal and no `d` unit: `-1h`, `1.5h` and
`7d` exit `2`, as does an empty value. `0s` is accepted by c1i and sent as is;
whether the server takes it is its call (it stores it for `--duration-grant` and
`tasks update-grant-duration`, and refuses it for `requests create grant`).
Ranges beyond that are the server's too and are noted per flag.

### Update masks

`--update-mask` takes comma-separated field paths, written as the API's
camelCase JSON names and sent as given (a snake_case path is a 400,
`FieldMask.paths contains invalid path`). Spaces around each path are trimmed.
An empty path (`a,,b`, a leading or trailing comma) or an empty dot-segment
(`a..b`, `.a`, `a.`) exits `2` and names its position. Unlike `--fields`, a
trailing or doubled comma is an error, because a mask scopes a write.

- `policies update`, `automations update`, `access-reviews update`, `access-reviews templates update`,
  `mcp classifiers update`, `mcp classifiers policy update` and
  `mcp classifiers tool-gates update` send the object in `--body-file` with
  that mask. `--body-file` requires `--update-mask`, and a blank mask exits `2`.
  On `policies update`, `automations update` and `access-reviews templates
  update`, which otherwise derive the
  mask from their flags, `--update-mask` without `--body-file` exits `2`.
- `mcp servers update-credentials` derives the mask from the config you supply
  when `--update-mask` is omitted; `--update-mask ""` counts as omitted. An
  explicit mask must name (or be nested under) every top-level config key the
  command sends, such as `url` for `--server-url`; otherwise it exits `2` naming
  the uncovered key, since the server would not apply it.
- `mcp servers test-connection` sends no mask when `--update-mask` is omitted
  (or `""`). A non-empty mask is edit mode only (`<connector-id> --app-id`);
  create mode exits `2`. In edit mode the same coverage rule applies.

### Soft delete

Most deletes mark the object rather than erase it, so a later `get` does not
always exit `4`:

- `apps delete`: the app is marked with `deletedAt`; `apps get` shows it, and
  `apps list` omits deleted apps.
- `entitlements delete`: `entitlements get` still returns it, with `deletedAt`
  set.
- `policies delete`: the policy leaves `policies list` and the default
  `search`, but `policies get` still returns it at exit `0` with `deletedAt`
  set; only `search --include-deleted` finds it in a listing. List and search
  rows carry `deleted_at`,
  `null` on a live policy, so `jq 'select(.deleted_at)'` selects only the
  deleted ones.
- `automations delete`: the API's own list stops returning the automation,
  but c1i's `automations list` reads the search index (the route c1i uses to
  page past the GET's first page), which can keep returning it with no deleted
  marker; `automations get` still returns it at exit `0` with no field marking
  it deleted.
- `access-profiles delete`: the profile leaves `access-profiles list`, while
  `access-profiles get` still returns it with `deletedAt` populated.
- `webhooks delete`: the webhook leaves `webhooks list`, but `webhooks get`
  still returns it at exit `0` with `deletedAt` set.
- `attribute-mappings delete`: reflected immediately. `attribute-mappings get`
  exits `4` and the list omits it; deleting again exits `0`.
- `service-principals delete`: reflected immediately. `service-principals get`
  exits `4` and the lists omit it; `users get <sp-id>` shows
  `"status": "DELETED"` and `deletedAt`.
- `service-principals credentials revoke`: `credentials get` on a revoked
  credential still returns the record, exit `0`, with no field marking it; only
  the lists omit it.
- `service-principals trusts delete` and `workload-federation providers
  delete`: `get` still returns the object, exit `0`, with no field marking it;
  only list and search omit it. Deleting again prints `{}`. Deleting a service
  principal deletes its trusts the same way.

### Field selection

`--fields` is the default for reducing JSON read output: use it when you know
the keys you need. It has no external dependency, keeps list output as NDJSON,
and understands c1i's snake_case/camelCase field variants.

Use `jq` only when the work is more than projection: conditional selection,
reshaping, aggregation, sorting, or extracting a value from mutation output
(with `jq -er`). `--fields` never changes mutation output.

```sh
# Known keys: prefer c1i's built-in projection
c1i users list --fields id,email

# Nested known keys: --fields preserves the object shape
c1i functions get <id> --fields id,displayName,publishedCommitId

# Conditional selection: jq is appropriate
APPS_WITH_USERS="$(c1i apps list | jq -c 'if .user_count > 0 then . else empty end')"

# Mutation output: never projected; extract the id with jq -er
APP_ID=$(c1i apps create --display-name "Example" | jq -er .id)
```

- Comma-separated; use dot-paths (`user.email`) for nested access. A trailing
  comma is fine, but a value that names no path (`,`, `.`, or `"$A,$B"` with
  both unset) or has an empty segment (`id.`, `a..b`) is a usage error (exit
  `2`), from `--fields`, `C1I_FIELDS` or `~/.c1i.yaml` alike; it used to turn
  projection off. `--fields ""` turns a session-wide `C1I_FIELDS` off.
- Matches the emitted keys, trying an **exact match first**, then falling back
  to a **case- and separator-insensitive** match. So `--fields displayName`
  resolves whether the output uses `displayName` (single-object reads) or
  `display_name` (list rows); the output keeps the source key's own spelling.
- Single-object `get` commands print the resource itself, so `--fields id`
  yields `{"id": ...}` and `jq -r .id` works. The API wraps the resource under
  its own key (`app`, `function`, `userView.user`, ...); `get` unwraps that and
  keeps every other envelope key — `expanded` among them — as a top-level
  sibling. Do **not** write the wrapper into a path: `--fields function.id`
  does not resolve, because there is no `function` key left.
- `c1i api` is a raw passthrough and still returns the envelope. There, and
  for any genuinely nested field, a name that doesn't match at the top level is
  also searched for deeper: the shallowest match wins, and a tie at the same
  depth resolves to the alphabetically first full path, deterministically.
- Mutation output is unwrapped the same way, but never projected: an explicit
  `--fields` on a mutation (even `--fields ""`) is a usage error (exit `2`),
  checked before anything is sent; `C1I_FIELDS` alone is ignored there. The id
  path varies by command (`.id`, `.connectorId`, `.srcAppId`, ...); a delete
  prints `{}`.
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
  - **Worst case:** a spec that matches nothing, with or without `--limit`,
    scans every page before exiting `2`. `accounts list --unmapped-only` and
    `functions usage` do the same, since their filters run after the fetch.
- Missing fields are silently omitted, so requesting a superset is safe.
- Also settable via `C1I_FIELDS`. Applies to read output — list commands, `api`,
  and single-object `get` commands. Mutation output is never projected, so a
  session-wide `C1I_FIELDS` is ignored there.

### Errors & exit codes

On failure, `c1i` writes an error to stderr and exits with a code an agent can
branch on without parsing text:

| Code | Meaning |
|------|---------|
| `0` | success |
| `1` | generic / unclassified error |
| `2` | usage error (bad flags or arguments, an empty, `.` or `..` id, a blank flag or positional, a missing, malformed, `null` or wrong-shape `--*-file` input, a non-object element in an array `--*-file` or `--body-file`, a referenced id that exists but is soft-deleted or of the wrong kind (a deleted or wrong-type policy), a control character in a path flag, header or `--config-field` key (see [Raw API](#raw-api)), a refused redirect (but `upgrade` and the `docs` commands exit `8`), `mcp gateway call`'s request was rejected with JSON-RPC `-32602` (invalid params, e.g. an unknown tool), or the API returned any `4xx` other than `401`/`403`/`404`/`408`/`429`/`499`) |
| `3` | not authenticated, or API returned `401`/`403` (including a `400`/`401`/`403` from the token endpoint; any other token-endpoint failure is classified like an API response: `404` exits 4, `429` 5, `5xx` or a `200` that is not a token 6) |
| `4` | API returned `404` (not found), or a referenced id (`--user-id`, a policy id) that does not exist, whether the server or a c1i pre-check catches it |
| `5` | API returned `429` (rate limited — back off and retry) |
| `6` | C1 failed: the API returned `5xx`, a redirect chain never settled, it answered `200` with a body that isn't JSON or a usable token, a list command's pagination cursor stopped advancing, or a list page in a shape the command doesn't recognize |
| `7` | `mcp gateway call` completed, but the tool itself reported an error (`isError: true` in its result) |
| `8` | a system beyond C1, or the MCP protocol layer, failed — an upstream connector was unreachable or errored, the gateway itself was unreachable (DNS failure, refused connection), `mcp servers test-connection` reported an EXTERNAL server unreachable (its JSON is still printed), or the gateway returned another protocol-level JSON-RPC error (`invalid params`, `-32602`, is exit `2`) |

Exit `1` with `writing output:` means stdout failed (e.g. a full disk). On a
mutation the message is `<METHOD> <path> succeeded but writing its response
failed`: the change was applied, so verify before retrying a mutation.

`6` and `8` are both "something remote broke," but they call for different
responses. `6` means C1 itself is failing, so retrying later is reasonable and
there is nothing to fix at your end. `8` means C1 answered fine and something
past it did not, so the same call will usually fail the same way: if a connector
is at fault, inspect it (`c1i mcp servers get <connector-id> --app-id <id>`, and
`mcp servers test-connection` for an EXTERNAL server) — it may be unreachable or
its credentials may have expired; another protocol-level JSON-RPC error
indicates a version mismatch or a bug in c1i, worth reporting rather than
working around (`invalid params`, `-32602`, is exit `2`: an unknown tool or bad
arguments, not a remote failure).

An **empty id argument** is a usage error, not a lookup: `c1i policies get ""`
exits `2` and sends nothing, because an empty id would render a trailing empty
path segment that the API redirects to the collection endpoint. An id of `.` or
`..` is refused the same way, as is one with a `;` suffix (`..;x`), which the
API treats as `..`. The same check applies to a raw `api --path` ending in `/`
or containing such a segment, percent-encoded (`%2E`) or not. Every positional
id and every flag ending `-id`, `-ids`, `-uid` or `-uids` refuses `.` and `..`
the same way, before credentials are loaded and under `--dry-run`.

The REST client is **selective about HTTP redirects**. It follows one only when
the target path is identical to what was requested (a trailing-slash difference
counts as a change) and the target host is in the same trust scope as the
request host: the same host differing only in scheme or port, or a
`label.`-prefix relationship in either direction with at least two labels,
which covers `apex ↔ www` canonicalization. An `https`→`http` downgrade is
never followed. Anything else is refused as an error (exit `2`) naming the
target. The path rule stops an id such as `/` or `.` from turning a
single-object read into a full listing; the host rule keeps your bearer token
from going to whatever host a redirect names, since a followed redirect is
re-authenticated. A chain of allowed redirects that doesn't settle within five
hops fails as a remote error (exit `6`) rather than looping.

This applies to every command that sends HTTP: the REST client, the MCP
gateway, the login handshake, `upgrade` and every `docs` command, so the path
and redirect guards, `--debug` tracing, and `--max-retries` cover all of them,
not just REST commands (`upgrade` and the `docs` commands map a refused
redirect to exit `8`, and `docs search`/`docs page` map any failure to `8`).

One narrower carve-out inside login: the device-flow token poll forces its own
retry count to zero, because RFC 8628's polling interval already *is* that
call's retry strategy and a second layer underneath would double the delays.
`--max-retries` still governs the rest of the handshake.

Pass `--error-format json` (or `C1I_ERROR_FORMAT=json`) to get a machine-readable
error object instead of the default `Error: <msg>` line. For API errors it
includes the status, method, path, and response body:

```sh
$ c1i api --path /api/v1/apps/<nonexistent-id> --error-format json
{"body":{"code":5,"message":"not found (request-id: ...)"},"error":"API error: API GET /api/v1/apps/<nonexistent-id> returned 404: ...","method":"GET","path":"/api/v1/apps/<nonexistent-id>","status":404}
```

The `body` is embedded as JSON when the API returned JSON, otherwise as a string.
A secret-named URL parameter or JSON field the server echoed back prints as
`[redacted]` in the error text and `body`; a JSON body with something masked is
re-encoded compactly, key order kept.

In JSON mode **stderr is NDJSON**: every line is one object. Errors carry
`"error"` (select them with `jq 'select(.error)'`), warnings and other notices
`"notice"` and `"kind"`, `--wait` progress `"progress"`, and `--debug` trace
lines `"debug"`. Text mode is unchanged.

An error from a command that writes also carries `"applied"` (`api` does not:
c1i can't tell whether an arbitrary call writes):

| `applied` | When |
|---|---|
| `false` | Nothing reached the server: a flag or argument error, an explicit `--fields`, a refused id or redirect, a failed pre-send lookup, a failure before the request was written, or an API `4xx` other than `408`/`499` |
| `"unknown"` | The request may have reached the server: an API `408`/`499` or `5xx`, a transport error or timeout after the request was written, or **any** failure after an earlier attempt was sent and retried (a `DELETE` that gets a `503` and then a `404` is `"unknown"`) |
| `true` | The change was applied: the response wasn't JSON, printing it failed, or a `--wait` after it failed |

`entitlements create` adds a `"steps"` array naming each attempted request, and
its `applied` is `true` once any step was written.

`mcp tools approve` reports each failed id as its own object
(`{"error":…,"id":"<tool-id>","applied":…}`) and ends with a summary object
that has no `id`:
`{"error":"approved N of M tools; …","applied":…,"succeeded":N,"failed":K,"notAttempted":[…]}`.
**A batch can exit non-zero with changes applied:** it exits with the first
failure's code while the ids before it stay approved, a failed stdout write
stops it (exit `1`), and `notAttempted` lists the ids it never sent.

Exit codes are unchanged.

## Command Reference

### Users

```sh
c1i users list [--query <text>] [--email <email>] [--status enabled|disabled|deleted] [--origin directory|local|system|all]... [--page-size N] [--page-token TOKEN] [--limit N]
c1i users get <user-id>
c1i users profile-types <user-id>
c1i users set-delegation <user-id> (--delegate-user-id ID [--start-at RFC3339] [--expire-at RFC3339] | --clear)
```

`users list` reads `POST /api/v1/search/users`, which with no `--origin` returns only directory-origin users: local-origin users (service principals among them) and system users are excluded. `--origin all` lists every user; repeat `--origin` with `directory`, `local`, `system` or a `USER_ORIGIN_*` value (as a row's `origin` prints) to pick origins (any other value is a usage error, exit `2`). Each row carries `origin` (e.g. `USER_ORIGIN_LOCAL`) when the server reports one.
`users profile-types` prints `{"profileTypes": [...]}` for a user across their
connected apps (empty on the user tried; an unknown user id also returned an
empty list at exit `0`).

`users set-delegation` designates a delegate, optionally for a window
(`--start-at`/`--expire-at`, RFC3339), and prints the binding. `--clear` removes
the delegation (it sends an empty delegate id, which the API describes as removing it). Tried against a tenant: an unknown delegate
or user id was `404` (exit `4`), an expiry before the start `400` (exit `2`), and
a past start or expiry and a self-delegation were accepted at exit `0`.
Clearing printed `{"item": null}`, repeatedly; on a user who had never had a
delegation it was `404` `provided delegated user id is empty`. `users get`
showed `delegatedUserId` empty while the start was still in the future.

### Apps

```sh
c1i apps list [--page-size N] [--page-token TOKEN] [--limit N]
c1i apps search [--query <text>] [--display-name <name>] [--app-id <id> ...] [--exclude-app-id <id> ...] [--policy-id <id> ...] [--only-directories] [--page-size N] [--page-token TOKEN] [--limit N]
c1i apps get <app-id>
c1i apps create --display-name <name> [--description <text>] [--grant-policy-id <id>] [--revoke-policy-id <id>] [--certify-policy-id <id>] [--identity-matching strict|display-name|custom]
c1i apps update <app-id> [--display-name <name>] [--description <text>] [--grant-policy-id <id>] [--revoke-policy-id <id>] [--certify-policy-id <id>] [--unbind-policy grant|revoke|certify]... [--identity-matching strict|display-name|custom] [--app-user-mapper-file <path|->]
c1i apps owners <app-id> [--entitlements] [--page-size N] [--page-token TOKEN] [--limit N]
c1i apps add-owner <app-id> (--user-id <id> | --owner-entitlement-id <id> [--owner-entitlement-app-id <id>])
c1i apps remove-owner <app-id> (--user-id <id> | --owner-entitlement-id <id> [--owner-entitlement-app-id <id>])
c1i apps set-owners <app-id> [--user-id <id> ...] [--owner-entitlement-id <id> ... [--no-user-owners]] [--owner-entitlement-app-id <id>] [--wait] [--wait-timeout 4m]
c1i apps delete <app-id>
c1i apps resource-types list --app-id <app-id> [--page-size N] [--page-token TOKEN] [--limit N]
c1i apps resource-types search [--query <text>] [--display-name <name>] [--app-id <id> ...] [--resource-type-id <id> ...] [--exclude-resource-type-id <id> ...] [--trait-id <id> ...] [--exclude-trait-id <id> ...] [--page-size N] [--page-token TOKEN] [--limit N]
c1i apps resource-types get <resource-type-id> --app-id <app-id>
c1i apps resource-types create --app-id <app-id> --display-name <name> --resource-type <kind>
c1i apps resource-types update <resource-type-id> --app-id <app-id> --display-name <name>
c1i apps resource-types delete <resource-type-id> --app-id <app-id>
c1i apps routing-rules list --app-id <app-id> [--limit N]
c1i apps routing-rules get <rule-id> --app-id <app-id>
c1i apps routing-rules create --app-id <app-id> --display-name <name> [--description <text>] [--condition <cel>] [--enabled] [--priority N] [--request-policy-id <id>] [--emergency-grant-policy-id <id>] [--emergency-grant-enabled] [--max-grant-duration <dur>] [--request-schema-id <id>]
c1i apps routing-rules update <rule-id> --app-id <app-id> [--display-name <name>] [--description <text>] [--condition <cel>] [--enabled[=false]] [--request-policy-id <id>] [--emergency-grant-policy-id <id>] [--emergency-grant-enabled[=false]] [--max-grant-duration <dur>] [--request-schema-id <id>]
c1i apps routing-rules delete <rule-id> --app-id <app-id>
c1i apps routing-rules reorder --app-id <app-id> --rule-id <id> [--rule-id <id> ...]
c1i apps usage-controls get <app-id>
c1i apps resources list --app-id <app-id> --resource-type-id <id> [--page-size N] [--page-token TOKEN] [--limit N]
c1i apps resources search [--query <text>] [--app-id <id> ...] [--resource-type-id <id> ...] [--resource-id <id> ...] [--exclude-resource-id <id> ...] [--trait-id <id> ...] [--exclude-trait-id <id> ...] [--owner-user-id <id> ...] [--unowned-only] [--page-size N] [--page-token TOKEN] [--limit N]
c1i apps resources get <resource-id> --app-id <app-id> --resource-type-id <id>
c1i apps resources create --app-id <app-id> --resource-type-id <id> (--display-name <name> [--description <text>] [--owner-id <id> ...] | --body-file <path|->)
c1i apps resources update <resource-id> --app-id <app-id> --resource-type-id <id> ([--display-name <name>] [--description <text>] [--custom-description <text>] | --body-file <path|-> --update-mask <paths>)
c1i apps resources delete <resource-id> --app-id <app-id> --resource-type-id <id>
c1i apps resources owners <resource-id> --app-id <app-id> --resource-type-id <id> [--entitlements] [--page-size N] [--page-token TOKEN] [--limit N]
c1i apps resources add-owner <resource-id> --app-id <app-id> --resource-type-id <id> (--user-id <id> | --owner-entitlement-id <id> [--owner-entitlement-app-id <id>]) [--wait] [--wait-timeout 4m]
c1i apps resources remove-owner <resource-id> --app-id <app-id> --resource-type-id <id> (--user-id <id> | --owner-entitlement-id <id> [--owner-entitlement-app-id <id>])
c1i apps resources set-owners <resource-id> --app-id <app-id> --resource-type-id <id> [--user-id <id> ...] [--owner-entitlement-id <id> ... [--no-user-owners]] [--owner-entitlement-app-id <id>] [--wait] [--wait-timeout 4m]
c1i apps managed-state list --app-id <app-id> --resource-type-id <id> [--page-size N] [--page-token TOKEN] [--limit N]
c1i apps managed-state get <resource-id> --app-id <app-id> --resource-type-id <id>
c1i apps managed-state promote <resource-id> --app-id <app-id> --resource-type-id <id> [--user-id <id> ...]
```

`user_count` is the platform's stored count, not a live tally: on some apps it differs from the number of `accounts list --app-id <app-id>` rows. To size a change, count those rows, not `user_count`.

`apps list` takes no filters; to narrow by name, id, policy or directory use `apps search` (`POST /api/v1/search/apps`), which emits the same rows. `--query` uses the server's full-text search over name and description (tokens, stems and 3-character n-grams, not simple substring matching; 1-2 character terms behave inconsistently), `--display-name` is a case-insensitive server match whose value is an ILIKE pattern (`%` and `_` are wildcards), and `--only-directories` sets the server's `onlyDirectories` filter. Each `--policy-id` is looked up first (unknown: exit `4`, deleted: exit `2`, nothing searched). Observed: an `--app-id` the tenant lacks matches nothing (empty result, exit `0`); `.`/`..` ids are refused locally and another malformed id is rejected by the server (exit `2`).

`apps create` needs only `--display-name`; it makes a plain, unmanaged
container app — the zero state for "make an app, then register MCP servers
under it". The caller is auto-assigned as an owner, showing up in `apps owners`
after the usual provisioning lag. The new app prints as pretty JSON, unwrapped
like `apps get`, so read its id with `jq -er .id` (`-e` fails on a `--dry-run`
preview, where `jq -r` exits 0 with `null`). An explicit `--fields` on a
mutation is a usage error (exit `2`).

`apps update` sends only the fields whose flags you pass, and prints the
updated app the same way. It reads the app first: a malformed id is a usage
error (the API 404s the update path), an unknown app exits `4` and a
soft-deleted one `2`, before anything is sent (`--dry-run` included).
`--grant-policy-id`, `--revoke-policy-id` and `--certify-policy-id` bind
policies, on `apps create` too (omitted at create,
the server assigns its default grant and revoke policies and no certify
policy). Before sending, c1i fetches each policy and refuses (nothing sent)
an id that doesn't exist (exit `4`), is deleted, or isn't of the flag's
type (exit `2`) (a `--dry-run` preview does it too). An empty policy id is a usage error, not
an unbind: unbind with `--unbind-policy grant|revoke|certify` (repeatable),
which can't be combined with setting the same kind.
`--identity-matching` takes `strict`, `display-name`, `custom`, or the full
`APP_USER_IDENTITY_MATCHING_*` name; any other value is a usage error (exit
`2`). `--app-user-mapper-file` replaces the whole mapper with a JSON object
such as
`{"mappingCases":[{"userKeyCel":"subject.email","appUserKeyCel":"account.email"}]}`
(`subject` is the C1.ai user, `account` the app user); the server rejects bad
CEL with a 400 (exit `2`). `mappingCases` must be non-empty: the server
accepts an empty mapper with exit `0` but keeps the old one, and the API has
no way to remove a mapper.

`apps delete` is a soft delete (see [Soft delete](#soft-delete)). It prints the
API's response, `{}`; exit `0` is the success signal.

`apps set-owners` replaces the whole list with exactly the ids you pass, so an
owner added between your read and your write is silently removed. It prints the
API's JSON response and returns as soon as the `PUT` is accepted. An unknown
but well-formed app id is answered `200 {}` too (measured; `apps get` and
`apps add-owner` 404 on the same id), so confirm the app first.

`apps owners` is the read that reflects what `apps add-owner`,
`apps remove-owner` and `apps set-owners` write. It returns zero rows at exit 0
for a well-formed but nonexistent app id, so an empty result can also mean
"wrong id". `apps add-owner` and `apps remove-owner` change one owner at a time
(a single `--user-id`; use `set-owners` for many). A repeated `--user-id` exits
`2` before any request, here, on `entitlements add-owner|remove-owner` and on
`accounts set-owner`. `apps add-owner` looks the user up first like `set-owners`
(an unknown one exits `4`, nothing sent); `remove-owner` does not, so a deleted
user can still be removed (and `--dry-run` previews an unknown one).

`apps managed-state` inspects and promotes the apps a connector discovered (an
identity provider's catalog): `--app-id` is the connector app and
`--resource-type-id` its resource type named `app` (`apps resource-types list`
shows it). `list` rows carry `resource_id`, `display_name`, `state`
(`unmanaged` or `managed`) and `managed_app_id` (null while unmanaged); a well-formed but unknown
app or resource type lists zero rows at exit `0`, while `get` exits `4`.
`promote` creates a managed app for `<resource-id>`, prints the binding
unwrapped like `get` (read the new app's id with `jq -er .state.managed.appId`),
and honors `--dry-run`. **Promote creates an app and nothing checks for an
existing one of the same name.** Promoting an already-managed binding exits `0`
and returns the existing app, not a second one. Without `--user-id` the new
app's owners were the connector app's owners; `--user-id` sets them instead
(looked up first; an unknown one exits `4`, nothing sent). After `apps delete`
of the promoted app the binding's soft-delete lags a few seconds, and a promote
in that window creates another app, so wait until `get` shows `deletedAt` set
(`promote` then exits `4`). The binding later reappears unmanaged, after which a
promote creates a new app. A binding's top-level `appId` is the connector app;
the managed app is `.state.managed.appId`. `c1i docs guide
promote-idp-app-to-managed` walks the whole workflow and has the timings.

`apps routing-rules` manages an app's entitlement routing rules (the API's
entitlement configuration rules). Rules are evaluated by `priority` (1 first);
the first enabled rule whose CEL `--condition` matches supplies the request
settings: `--request-policy-id`, `--emergency-grant-policy-id` with
`--emergency-grant-enabled`, `--max-grant-duration` and `--request-schema-id`.
An enabled rule routes real requests; `create` makes a disabled rule unless
`--enabled` is given. `create --priority` is a 1-indexed insertion slot: later
rules move down one, and a slot past the end (or omitting it) appends.
`reorder --rule-id` takes every active rule's id once, in the new order, and
assigns priorities 1..N; a missing, extra, repeated, unknown or deleted id is a
`400` (exit `2`), `rule_ids must be a permutation of active rules for the app:
...`. `delete` is soft (`get` still returns the rule with `deletedAt`) and
renumbers the rest; a repeat delete exits `4`. An app held at most 10 rules when
measured; past that `create` exits `2` with `This app already has the maximum of
10 entitlement configuration rules. Delete a rule to add another.` The server
checks referenced ids: an unknown policy or request schema exits `4`, a
non-grant policy `2`. `list` takes no paging and answers an unknown app with
zero rows (exit `0`); the other commands exit `4` for an unknown app or rule.
`create`, `update` and `reorder` read the app first: a deleted app exits `2`
before anything is sent, `--dry-run` included. The update API has no mask and
replaces the whole rule, so `update` reads the rule and sends every writable
field with only your flags changed; a change made between that read and the
write is overwritten. `update` refuses a deleted rule (exit `2`), cannot
change the order (use `reorder`), and cannot unset a policy or schema id.
`apps usage-controls get <app-id>` shows the
notify and revoke-on-non-use controls beside `hasUsageData`; an app whose
controls were never set prints only `hasUsageData`. The endpoint answers an
unknown app like an app with no controls, so c1i reads the app first (exit
`4`; a deleted app exits `2`). Updating usage controls answered `501` (`feature is disabled`) on the
tenant tested, so there is no `update` command.

`apps resource-types` and `apps resources` manage an app's resource types and
the resources under them; `entitlements create` builds the same objects in one
call when you also want an entitlement. The resource's own id is positional;
`--app-id` (and `--resource-type-id` for a resource) is its parent scope.
`create`, `update` and `delete` act on manually managed objects. Both `delete`s
are soft (see [Soft delete](#soft-delete)): `get` still returns the object
with `deletedAt` set, while `list` and `search` stop returning it. `list` and
`search` can trail a write by a few seconds. The API spec says a type delete also deletes its
resources, but a resource of a deleted type was still live ten minutes later, so
delete resources first if they must go. `resource-types create --resource-type`
has no default and takes the API spec's kinds (`ROLE`, `GROUP`, `LICENSE`,
`PROJECT`, `CATALOG`, `CUSTOM`, `VAULT`, `PROFILE_TYPE`, `SESSION_POLICY`,
`CLAW_AGENT`); only `CUSTOM` can repeat on one app (otherwise a 500,
`app resource type already exists`, exit `6`). `resource-types update` renames
only: the API answered a `traitIds` change `200` and left it unchanged.
`resources create` looks up each `--owner-id` first, and an unknown one exits
`4` where the server would 500. It looks up the resource type too: a type that
does not exist exits `4`. A deleted type exits `2` where the server would 500. `resources update`
sends only the flags you pass; `--custom-description ""` clears that field, while
the server ignores an empty name or description, so those exit `2` when
blank. Other fields (such as `annotations`, which were merged rather than
replaced) go through `--body-file` with `--update-mask`; `resources create
--body-file` sends a full request, except that it must not set `appId`/
`app_id` or `appResourceTypeId`/`app_resource_type_id` (exit `2`): the flags
own them, and the server ignored the body values.
Search rows have the same shape as the matching `list`. The id filters
(`--resource-type-id`, `--exclude-resource-type-id`, `--resource-id`,
`--exclude-resource-id`, `--trait-id` and `--exclude-trait-id`, which match the
`trait_ids` of a type) keep or drop rows; `--owner-user-id` keeps resources any
given user owns and `--unowned-only` those with none. Every unknown filter id
tried matched nothing (exit `0`). One resource type or resource id can appear
under several apps, so key search rows on `app_id` (and `resource_type_id`)
plus `id`. `apps resources owners`, `add-owner`, `remove-owner` and
`set-owners` follow the app and entitlement owner commands below, on the
`.../resource_types/{resource-type-id}/resource/{resource-id}/owners` route.
The API answers `set-owners` on an unknown resource `200 {}` and stores the
owners anyway, so c1i looks the resource up first (exit `4`). The owners
response also carries `immutableUserIds` (owners the API says cannot be
removed), which the rows leave out; read it with `c1i api`.

**Owner commands (apps, entitlements and resources).** Owner changes are applied
asynchronously and can take several minutes to appear; pass --wait to block
until they do. `--wait` (on `set-owners`, and on `entitlements add-owner` and
`apps resources add-owner`)
polls `GET .../ownerids` until the change lands (`add-owner`: the owner
appears; `set-owners`: the owner set equals the requested ids, removals
included) or `--wait-timeout` (default `4m`) elapses. Its progress lines go to
stderr, so stdout stays the write's JSON response. A wait failure reports
`"applied": true` under `--error-format json`. A timeout exits `1` even though
the write was accepted, so re-check with `owners` rather than re-issuing the
write. With `--dry-run` the preview still only covers the write; `--wait` never
polls. A `--wait-*` flag given without `--wait` (an explicit `--wait=false` is fine) is a usage error (exit 2).

`set-owners` looks up each distinct `--user-id` with `GET /api/v1/users/{id}`
before the `PUT`: an unknown one exits `4` naming it and nothing is sent
(a `--dry-run` preview looks them up too), and a repeated `--user-id` is sent once. The
API would otherwise accept an unknown id at exit `0`, so a set naming only
unknown ids would leave no real owners: an app stores the id (`GET .../ownerids`
lists it, `apps owners` does not), an entitlement drops it.

**Entitlement owners.** An owner can also be an entitlement, whose grantees own
the app, entitlement or resource. `add-owner` and `remove-owner` take
`--owner-entitlement-id` instead of `--user-id` (exactly one of the two), and
`--owner-entitlement-app-id` when the entitlement is in another app than the
owned object. `owners --entitlements` lists them (rows: `id` of the owning
entitlement, `app_id`, `display_name`, `slug`, `alias`, `role_slug`,
`added_at`, `deleted_at`). These go through the v2 owner routes
(`/api/v2/.../owners/entitlements/primary/{app-id}/{entitlement-id}`) under
the role `primary`, the only role the API supports. Adding an entitlement that
is already an owner, or removing one a second time, exits `0`; an unknown
entitlement or owned object exits `4`, and so does `owners --entitlements` on
an unknown id. `add-owner --owner-entitlement-id` refuses `--wait` (exit `2`):
the owner is in place when it returns.

`set-owners` with any `--owner-entitlement-id` sends the v2 set, which
replaces the entitlement owners and is meant to replace the user owners with
the `--user-id` values. c1i refuses it (exit `2`, nothing sent, `--dry-run`
included) unless you repeat `--user-id` for each user owner to keep or pass
`--no-user-owners` to remove them all; `--no-user-owners` needs
`--owner-entitlement-id` and excludes `--user-id`. The user part was not
reliably reflected by the v1 owner reads: for an app, `apps owners` and `GET
.../ownerids` caught up 1-2 minutes later; for a resource, a user owner the
set removed was still listed more than 8 minutes later. So `--wait` in this
form polls `GET .../ownerids` for the users and the v2 entitlement owner list
until both equal the request, and a timeout exits `1` (the write was
applied). Without `--owner-entitlement-id`, `set-owners` replaces user owners
only and leaves entitlement owners in place. The v2 connector, account and
user owner routes have no commands: on the tenant tried, each answered
`ownership v2 is not enabled`.

### Accounts

```sh
c1i accounts list --app-id <id> [--status enabled|disabled|deleted] [--type user|service-account|system-account] [--unmapped-only] [--query <text>] [--page-size N] [--page-token TOKEN] [--limit N]

c1i accounts set-owner <app-user-id> --app-id <id> --user-id <id>
```

`accounts set-owner` looks the `--user-id` user up first: an unknown one exits `4`
and nothing is sent (`--dry-run` looks it up too, so the lookup needs user read).

`accounts set-owner` prints the app user as pretty JSON, unwrapped like the
reads: `jq -er .id` is the account and `.identityUserId` its owner.

### Entitlements

```sh
c1i entitlements list [--app-id <id>] [--query <text>] [--page-size N] [--page-token TOKEN] [--limit N]
c1i entitlements get <entitlement-id> --app-id <id>
c1i entitlements delete <entitlement-id> --app-id <id>
c1i entitlements owners <entitlement-id> --app-id <id> [--entitlements] [--page-size N] [--page-token TOKEN] [--limit N]
c1i entitlements add-owner <entitlement-id> --app-id <id> (--user-id <id> | --owner-entitlement-id <id> [--owner-entitlement-app-id <id>]) [--wait] [--wait-timeout 4m]
c1i entitlements remove-owner <entitlement-id> --app-id <id> (--user-id <id> | --owner-entitlement-id <id> [--owner-entitlement-app-id <id>])
c1i entitlements set-owners <entitlement-id> --app-id <id> [--user-id <id> ...] [--owner-entitlement-id <id> ... [--no-user-owners]] [--owner-entitlement-app-id <id>] [--wait] [--wait-timeout 4m]
c1i entitlements grants <entitlement-id> --app-id <id> [--page-size N] [--page-token TOKEN] [--limit N]

# Membership automation (C1 app entitlements only)
c1i entitlements automation get <entitlement-id> --app-id <id>
c1i entitlements automation create <entitlement-id> --app-id <id> --display-name <name> [--description <text>] \
  [--cel <expr> | --refs-file <file> | --none]
c1i entitlements automation create <entitlement-id> --app-id <id> --body-file <file>
c1i entitlements automation update <entitlement-id> --app-id <id> [--display-name <name>] [--description <text>] \
  [--cel <expr> | --refs-file <file> | --none]
c1i entitlements automation delete <entitlement-id> --app-id <id>
c1i entitlements automation exclusions list <entitlement-id> --app-id <id> [--page-size N] [--page-token TOKEN] [--limit N]
c1i entitlements automation exclusions add <entitlement-id> --app-id <id> --user-id <id> [--user-id <id> ...]
c1i entitlements automation exclusions remove <entitlement-id> --app-id <id> --user-id <id> [--user-id <id> ...]

# Directional entitlement-to-entitlement proxy binding
c1i entitlements proxy-bindings get \
  --source-app-id <id> --source-entitlement-id <id> \
  --destination-app-id <id> --destination-entitlement-id <id>
c1i entitlements proxy-bindings create \
  --source-app-id <id> --source-entitlement-id <id> \
  --destination-app-id <id> --destination-entitlement-id <id>
c1i entitlements proxy-bindings delete \
  --source-app-id <id> --source-entitlement-id <id> \
  --destination-app-id <id> --destination-entitlement-id <id>

# Create one on a manually-managed app, with its resource type and resource
c1i entitlements create --app-id <id> --display-name "Payroll admin" \
  [--description <text>] [--slug member] [--alias payroll_admin] [--owner-id <user-id>] \
  [--duration-grant 3600s] [--resource-type CUSTOM] \
  [--resource-type-display-name "Payroll role"] [--resource-display-name "Payroll admins"] \
  [--grant-policy-id <id>] [--revoke-policy-id <id>] [--certify-policy-id <id>] \
  [--emergency-grant-policy-id <id>]

# Reuse an existing resource type (or an existing resource) instead of creating one
c1i entitlements create --app-id <id> --display-name "Payroll viewer" --resource-type-id <id>
c1i entitlements create --app-id <id> --display-name "Payroll viewer (RO)" \
  --resource-type-id <id> --resource-id <id>

# Update name, description, policies, or max grant duration (only the flags passed)
c1i entitlements update <entitlement-id> --app-id <id> \
  [--display-name <name>] [--description <text>] \
  [--grant-policy-id <id>] [--revoke-policy-id <id>] [--certify-policy-id <id>] \
  [--emergency-grant-policy-id <id>] [--emergency-grant-enabled[=false]] \
  [--unbind-policy grant|revoke|certify|emergency-grant] \
  [--duration-grant 8h | --duration-unset]
```

`entitlements delete` is a soft delete (see [Soft delete](#soft-delete)) and
prints `{}`. `apps delete` does not delete an app's entitlements, so remove
them with this first. The server answers `200 {}` for an unknown entitlement id
or a `--app-id` the entitlement does not belong to (measured), so confirm with
`entitlements get` rather than trusting exit 0.

`entitlements update` builds the update mask from exactly the flags you pass.
`--description ""` is sent, and the server restores the description the
entitlement was created with (empty when it had none) — not a general clear,
and not the same as `apps update`, which does clear.
Each policy id is checked before anything is sent: it must exist, not be
deleted, and be of the slot's type: an unknown policy exits `4`, a deleted or
wrong-type one `2`.
`--emergency-grant-policy-id` takes a grant policy; the server enforces that
only when the same call enables emergency grants (`emergency policy id must
reference a grant policy`) and otherwise stores any id. An empty policy id is
exit `2`; unbind with the repeatable `--unbind-policy`.
`--emergency-grant-enabled=true` needs `--emergency-grant-policy-id` in the same
call even when one is already stored (the server's `emergency grant policy id
must be set if emergency grant is enabled`), so it exits `2` without one or
alongside `--unbind-policy emergency-grant`. To unbind and disable together,
pass `--emergency-grant-enabled=false` with the unbind. Unbinding the emergency
policy while emergency grants stay enabled is accepted by the server.
`--duration-grant` follows the [Durations](#durations) rule; `--duration-unset`
removes the limit, and the two are mutually exclusive. A blank `--display-name`
is refused: the server would accept "" and restore the entitlement's original
name.
`entitlements create` checks its policy flags the same way before its first
`POST`, so a bad policy creates nothing.

An entitlement points at an app resource, which lives under an app resource
type, so `entitlements create` is up to three `POST`s: the resource type, the
resource, then the entitlement. `--resource-type-id` and `--resource-id` skip
whichever of the first two steps you already have — one resource type can carry
many resources, and one resource many entitlements. The server requires both
ids on the entitlement even though the OpenAPI schema marks only `displayName`
required, so `--resource-id` without `--resource-type-id` is rejected at exit
`2` before anything is sent.

`--resource-type` is `ROLE`, `GROUP`, `LICENSE`, `PROJECT`, `CATALOG`,
`CUSTOM`, `VAULT`, `PROFILE_TYPE`, `SESSION_POLICY` or `CLAW_AGENT` (the API
spec's kinds; case-insensitive here, uppercase on the
wire; defaults to `CUSTOM`) and describes the resource type this command creates, so passing it
together with `--resource-type-id` is a usage error rather than a silently
ignored flag. Only `CUSTOM` can repeat on one app: a second resource type of
any other kind fails with a 500 (exit `6`, though retrying never helps) saying
`app resource type already exists`, so reuse the existing one with
`--resource-type-id` and drop both `--resource-type` and
`--resource-type-display-name` — either one alongside the id is a usage error.
Reusing a resource with `--resource-id` likewise means you drop
`--resource-display-name`. `--owner-id` is repeatable and goes inline in the
create request, so no follow-up call is needed; an empty one is a usage error
rather than an owner quietly dropped, and each id is checked before the first
create (an unknown user exits `4` and creates nothing; the check reads the user,
so a credential without read access exits `3`, a `--dry-run` preview included). `--duration-grant` follows the [Durations](#durations)
rule; omit it for standing access.

`--dry-run` previews all three requests in `requests[]`; where an id only
exists after a real preceding step, `NEW_APP_RESOURCE_TYPE_ID` and
`NEW_APP_RESOURCE_ID` appear literally in the `url` and `body`, and a stderr
notice explains them. There is no rollback: if a later step fails, the objects
the earlier ones created still exist, and the error names them along with the
flags that reuse them and the create-only flags the retry has to drop. The
created entitlement is printed as pretty JSON, unwrapped like
`entitlements get` (`jq -er .id`; an explicit `--fields` is a usage error); it
echoes `appResourceTypeId`/`appResourceId` and expands both objects, so every
id the command touched is in that one payload. Under `--error-format json` a
failed run reports `applied` (`true` once any step was written) and a `steps`
array naming each attempted request.

`entitlements owners` lists an entitlement's owners (same row shape as
`apps owners`) and is the read that reflects `entitlements add-owner`,
`remove-owner` and `set-owners`; for the provisioning lag, `--wait` and the
`set-owners` user lookup, see **Owner commands** under Apps.

`set-owners` replaces the whole list and needs at least one `--user-id`, since
the API takes an empty list as "remove every owner"; `remove-owner` can still
remove the last owner. A repeated `--user-id` is sent once because the API
answers a duplicate with a 500. An unknown user, entitlement or app exits `4`
on `add-owner` (c1i looks the user up first, so `--dry-run` refuses it too). `remove-owner` exits `4` on a user who was never an owner as
well as on an unknown one, so `4` cannot tell those apart; a user who was an
owner and is already removed exits `0`. `owners` returns zero rows at exit `0`
for a nonexistent entitlement or app.

`entitlements grants` lists one entitlement's grants (`GET .../grants`) with
the account, the C1 user (`user_display_name`, `user_email`), `discovered_at`,
`expires_at` (the API's `expired`: in testing, the future end time set by
`grants update-duration`; null for a standing grant and once that time had
passed) and `grant_reason_count`. Like `owners`, it returns zero rows at exit
`0` for a nonexistent entitlement or app.
The deprecated `GET .../users` it replaces is not wrapped; its only extra field
was `originatingTicketId` (use `c1i api` if you need it).

`entitlements automation` manages the one automation rule an entitlement can
have. Only entitlements of the C1 app take one; any other is refused with
`only the C1 app can have entitlement automation` (exit `2`). The condition is
one of `--cel`, `--refs-file` (a JSON array of `{"appId","id"}`; the server
takes 1 to 32, and c1i looks each up first because the server stores an unknown
one, so an unknown reference exits `4` and a soft-deleted one `2`) or
`--none`; `create --body-file` sends a whole
`AppEntitlementAutomation` object and is the only way to set `basic`. `update`
derives its mask from the flags; the server needs a display name on every
update, so without `--display-name` c1i reads the automation and sends its
current name. `get` exits `4` for an entitlement without an automation. Delete
is soft: `get` then returns it with `deletedAt` set, and `update` and
`exclusions add|remove` still exit `0` against it (the printed automation keeps
`deletedAt`), so exit `0` does not mean the rule is live. In testing the server
refused a second `create` on the same entitlement with a 409, `conflicting
concurrent write, retry the request`, even after the first was deleted.
`exclusions add|remove` take C1 user ids; the server answers an unknown one with
`200` and does nothing, so c1i looks each up first (exit `4`). An automation
acts on real users as soon as it exists.

`entitlements proxy-bindings` manages directional entitlement-to-entitlement
visibility and tracking links. Source and destination are each identified by
their app and entitlement IDs; entitlement IDs are app-scoped. Creating a
binding does not grant access or configure delegated provisioning. Use
`c1i docs guide delegate-entitlement-provisioning` for the separate ordered
workflow that configures delegation on the destination entitlement. The public
REST API has no proxy-binding list endpoint; the C1 Console's binding browser
uses an internal gRPC-web search service that is outside the public API
contract.

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
than left to time out. A timeout exits `1` and prints no rows.

**An empty result is stable** and settles in about 10s with exit `0`. After a
write, pass `--wait-min 1` (or the count you expect) so exit `1` means "did not
converge in time", not "absent". After a revoke, exit `0` that still lists the
row means "not yet, re-run"; no flag helps there, since `--wait-min` is a floor
and cannot express a ceiling.

`--wait` settles on the whole matching set, fetching every page on every poll
regardless of `--limit`; `--limit` only truncates what is printed. Filter
narrowly. `--wait` and `--page-token` are mutually exclusive.

A grant row is not durable: in one run the grant search kept returning it with
`entitlement_deleted_at: null` immediately after its entitlement's delete, then
omitted it about 15 seconds later, so record `identity_user_id` before deleting
an entitlement if you need the inventory. Rows carry `entitlement_deleted_at`
and `app_user_deleted_at`; absent timestamps are `null`, never `""`.

#### Granting directly

```sh
c1i grants add --app-id <id> --entitlement-id <eid> --user-id <uid> [--user-id <uid> ...] [--wait]
```

`grants add` makes each user a manually managed member of the entitlement in
one request (`POST .../add-manual-user`). It **bypasses the grant policy**:
there is no approval step and no request task. For a grant that goes through
policy use `requests create grant`. A repeated `--user-id` is sent once. Each
user is looked up first, so an unknown user exits `4` with nothing sent
(`--dry-run` included; the lookup needs user read, else exit `3`); the server
itself answers an unknown user with `200` and does nothing. An unknown
entitlement or app exits `4` on a real run; `--dry-run` does not look them up.
The response (`bulkActionId`, `failedUsersErrorMap`) is printed as JSON. If
`failedUsersErrorMap` is not empty, the response is still printed, each
rejected user and its message is reported, and the exit is `1`; `--wait` then
waits only for the users that were not rejected (none left: no wait).

The grant lands asynchronously (seconds in testing, but reads can lag). `--wait`
polls the entitlement's grants until every user appears, or `--wait-timeout`
(default `4m`) elapses; a timeout exits `1`, though the write was accepted.

#### Changing a grant directly

```sh
c1i grants accounts --app-id <id> --entitlement-id <eid> --user-id <uid>
c1i grants remove --app-id <id> --entitlement-id <eid> --app-user-id <auid>
c1i grants update-duration --app-id <id> --entitlement-id <eid> --app-user-id <auid> --deprovision-at 2026-12-01T00:00:00Z
c1i grants remove-duration --app-id <id> --entitlement-id <eid> --app-user-id <auid>
```

`grants accounts` lists, one row per account, which of a C1 user's accounts in
the app hold the entitlement (zero rows at exit `0` for an unknown id). The
other three address one grant by its account, the `app_user_id` of `grants
list`.

`grants remove` (`DELETE .../remove-membership`) prints the API's response;
per the API, its `revokeTaskId` is set only for an access profile. Only refusals
were observed, never a successful removal: in testing it refused manually
managed entitlements (custom and group resource types) with `entitlement is not an SSO application
sign-in entitlement` (exit `2`), an unknown `--app-user-id` included; use
`requests create revoke` there, whose task on a manual entitlement waits on a
provisioning step. An unknown entitlement or app exits `4`.

`grants update-duration` sets when C1 starts the revoke (`--deprovision-at`,
RFC3339; the server refuses a past time with `new_deprovision_at must be in the
future`); on a manually managed entitlement that opened a revoke task in
testing, and the grant stayed listed while the task was open.
`remove-duration` makes it standing again. Both print the grant
(`.deprovisionAt`), and the change shows as `deprovision_at` in `grants list`.
An unknown app, entitlement or account, or an account without the grant, exits
`4`. For a grant still being requested use `tasks update-grant-duration`.

### Tasks

```sh
c1i tasks list [--state open|closed] [--query <text>] [--assigned-to-me] [--page-size N] [--page-token TOKEN] [--limit N]
c1i tasks get <task-id>
c1i tasks approve <task-id> [--policy-step-id <id>] [--comment <text>]
c1i tasks deny <task-id> [--policy-step-id <id>] [--comment <text>]
c1i tasks comment <task-id> --comment <text>
c1i tasks close <task-id> [--comment <text>]
c1i tasks reassign <task-id> --to-user-id <id> [--to-user-id <id> ...] [--policy-step-id <id>] [--comment <text>]
c1i tasks restart <task-id> [--policy-step-id <id>] [--comment <text>]
c1i tasks reset <task-id> [--comment <text>]
c1i tasks skip-step <task-id> [--policy-step-id <id>] [--comment <text>]
c1i tasks process <task-id>
c1i tasks update-grant-duration <task-id> --duration <duration>
c1i tasks escalate <task-id> [--policy-step-id <id>] [--comment <text>]
c1i tasks retry-provisioning <task-id> [--policy-step-id <id>] [--comment <text>]
c1i tasks update-request-data <task-id> --data-file <path|->
c1i tasks audits <task-id> [--comments-only | --exclude-comments] [--newest-first] [--page-size N] [--page-token TOKEN] [--limit N]
```

`restart`, `reset` and `skip-step` each rotate the task's current policy step,
so a `--policy-step-id` captured before one of them goes stale — the server
answers `this action is no longer available: the request has advanced to a new
approval step`. Omit the flag to act on whatever step is current.

`skip-step` refuses the current step when it is not an approval step (exit
`2`, nothing sent). Skipping a provision step abandons provisioning, so the
task can stay on that step (no grant, no outcome) or close depending on the
policy; name the step with `--policy-step-id` only if you intend that.

Which actions a task accepts depends on its state; the server refuses the rest
with `action not permitted`. Read the task's own list with
`c1i api --path /api/v1/tasks/<task-id> --fields actions`.

`restart` re-runs the current approval step; `reset` restarts the whole policy.
Either can reopen a closed task, but whether it does depends on how the task
closed: `reset` leaves a DENIED one CLOSED (exit `0`, outcome DENIED), and
`restart` answers `task is closed` (exit `2`) on a CANCELLED or DENIED one.
Neither is a general undo: re-read with `c1i tasks get` instead of trusting
either exit. `process` is intended for a stalled task and changes nothing
observable on a healthy one.

`update-grant-duration --duration` follows the [Durations](#durations) rule. It
only applies before the task reaches provisioning, after which the server
answers `cannot update grant duration for a ticket in a provision step`; the
value lands at `.type.grant.grantDuration` (there is no top-level
`.grantDuration`).

`escalate` moves a grant task onto its entitlement's emergency-grant policy
(afterwards `.emergencyAccess` is true); a task with no emergency-grant policy,
or one already escalated, is refused with `action not permitted`.
The API describes `retry-provisioning` as re-driving a failed provision step;
only its refusal has been observed (`action not permitted` on a task at an
approval step). Both look up the current step like `approve`, and send the id
that lookup returns, so a numeric task id works; with `--policy-step-id`
nothing is read and the 27-character id is required.
The API describes `update-request-data --data-file` as submitting the request
form of a task at a form step (keys are the form's field names,
`.policy.current.form.form.fields[].name`); a successful submission has not
been observed. An object that does not fit the form gets `request data does not
match the required schema (field 'Reason' is required)`; a valid one on a
self-requested form task whose `.actions` lacked
`TASK_ACTION_TYPE_UPDATE_REQUEST_DATA` was refused with `action not
permitted`; a task at another step gets `current policy step is not a form
instance`. It reads nothing first, so it needs the 27-character task id.

`audits` prints the task's audit trail as NDJSON, one raw event per row, oldest
first (`--newest-first` reverses it). `--comments-only` and
`--exclude-comments` filter comment events and together exit `2`. The task is
read first, because the audit endpoint answers an unknown task with an empty
list: an unknown id exits `4`. That read accepts a numeric task id, and the id
it returns is the one sent.

`approve-with-step-up` is not wrapped (it returns a browser `redirectUrl` to
finish step-up authentication interactively); reach it through `c1i api`.

`approve`/`deny`/`reassign`/`restart`/`skip-step` target a specific policy step, fetched
when `--policy-step-id` is omitted. `approve`, `skip-step` and `reassign`
require a resolvable step: a task with no current step exits `2` and tells you
to re-read it with `tasks get` (`--policy-step-id` cannot supply one). `deny` is
refused by the server in the same window (exit `2`; its message varies, e.g. a
missing-step validation error or `already approved`), so retry once the task
shows a step; `restart` resolves the step the same way. Because each of them reads the task first, an unknown task id exits `4`
before any action is sent. A failed step lookup (any error reading the task,
such as a `5xx`) fails the action and nothing is sent.

`close` cancels a task without recording an approval decision, and takes no
step. Closing an already-closed task is rejected by the API with `task is
closed` (exit 2).

`reassign` sets the step's approvers to the users named by `--to-user-id`;
repeat the flag to assign several. Before sending it looks each id up: an
unknown user exits `4` and nothing is sent, because the API would store the id
as the step's approver; a `--dry-run` preview looks them up too. A repeated id is sent once; recover a step already given a
bad approver by re-running `reassign` with valid ids (a system owner can also
`reset`/`restart` where the task offers them).

Every task action prints the task unwrapped like `tasks get` (`jq -er .id`),
plus `ticketActionId` (`taskActionId` on `close`) where the API returns one. The
endpoints return the task as it was *before* the action, so `.state` is a
snapshot: closing an open task prints `TASK_STATE_OPEN`. Re-read with `tasks
get` before acting on it. A policy step c1i looked up for you is reported on
stderr (`Resolved policy step: <id>`), never in the document.

### Connectors

```sh
c1i connectors list --app-id <id> [--page-size N] [--page-token TOKEN] [--limit N]
c1i connectors get <connector-id> --app-id <id>
c1i connectors create --app-id <id> --catalog-id <id> [--display-name NAME] [--description TEXT] [--user-id <id>]...
c1i connectors create --app-id <id> --body-file <file>
c1i connectors update <connector-id> --app-id <id> --body-file <file> --update-mask config [--justification TEXT]
c1i connectors delete <connector-id> --app-id <id>
c1i connectors sync <connector-id> --app-id <id>
c1i connectors sync-status <connector-id> --app-id <id>
c1i connectors sync-history <connector-id> --app-id <id> [--runs N]
c1i connectors sync-errors <connector-id> --app-id <id> [--top-k N]
c1i connectors sync-changes <connector-id> --app-id <id> [--since TIMESTAMP]
c1i connectors sync-compare <connector-id> --app-id <id> --left <lifecycle-id> --right <lifecycle-id>
c1i connectors sync-compare <connector-id> --app-id <id> --body-file <file>
c1i connectors debuggable [--page-size N] [--page-token TOKEN] [--limit N]
c1i connectors pause <connector-id> --app-id <id>
c1i connectors resume <connector-id> --app-id <id>
c1i connectors schedule <connector-id> --app-id <id> --cron "0 */6 * * *" [--timezone America/Chicago] [--justification TEXT]
c1i connectors schedule <connector-id> --app-id <id> --clear
c1i connectors credentials rotate --app-id <id> --connector-id <id>
c1i connectors credentials get <credential-id> --app-id <id> --connector-id <id>
c1i connectors credentials revoke <credential-id> --app-id <id> --connector-id <id>
```

The `sync-*` diagnostics are reads that print the API's response object as
returned (pretty JSON): `sync-history` (recent runs and `.streak`; `--runs` max
50), `sync-errors` (distinct redacted errors; `--top-k` max 20), `sync-changes`
(metadata-only change timeline from `--since`, an RFC 3339 timestamp) and
`sync-compare` (per-type count deltas between two runs' `.lifecycleId`s from
`sync-history`; the draft-test form takes `--body-file` and was not exercised
live). `debuggable` lists, as NDJSON across all apps, the connectors you may
run them on. An unknown well-formed id exits `4`, a malformed one `2`.

`connectors create` makes a connector with an empty config (the API's
delegated create; a malformed catalog id is a 400 (exit 2), a well-formed id
the catalog lacks can be a 500 (exit 6), and an MCP catalog id is a 400 — use
`mcp servers register`); set the config with `connectors update --body-file <file>
--update-mask config`, where the file is a `connectors get` output with
`.config` edited and the view keys removed (`jq 'del(.expanded, .appPath,
.capabilitiesPath, .usersPath)'`; the server refuses them as unknown fields).
In the runs checked, Update required the object to carry
`config` and `catalogId`, and a mask of `displayName` or `description` returned
200 without changing either. `connectors delete` reads the connector first (an
unknown id exits 4, `--dry-run` included) and refuses one that backs an MCP
server (exit 2): deleting that through this route left its toolsets and their
entitlements live, so use `mcp servers delete`.

`sync` queues a sync and exits 0 when it is accepted, not when it finishes;
follow it with `sync-status`. On a paused connector the server can accept the
call but queue nothing, so resume the connector first. `pause` and `resume` exit 0 when repeated.
`schedule --clear` removes the schedule; c1i does not validate `--cron` or
`--timezone`, and the server accepted invalid values in the run checked.
`--justification` is sent as `justification.reason`.

There is no list of a connector's credentials: an id comes from `credentials
rotate`, which prints `.clientSecret` once. The server resolves the credential
id globally and ignores the path's app and connector, so `credentials get`
refuses one whose `appId`/`connectorId` differ from the flags (exit `4`). In
the run checked, rotating left the previous credential valid for seven more
days (`.expiresTime`), and revoke set `.expiresTime` in the past while
`credentials get` still returned it.

### Functions

```sh
c1i functions list [--published-only | --draft-only] [--page-size N] [--page-token TOKEN] [--limit N]
c1i functions get <function-id>
c1i functions source <function-id> [--commit <id>] [--out-dir <path>]
c1i functions commits <function-id> [--page-size N] [--page-token TOKEN] [--limit N]
c1i functions usage <function-id> [--page-size N] [--page-token TOKEN] [--limit N]
c1i functions create --display-name NAME [--description TEXT] [--file main.ts]... [--commit-message MSG]
c1i functions update <function-id> [--display-name NAME] [--description TEXT] [--publish-commit <commit-id>] [--file main.ts]... [--commit-message MSG]
c1i functions update <function-id> --body-file function.json --update-mask outboundNetworkAllowlist
c1i functions delete <function-id>
c1i functions invoke <function-id> [--commit-id <id>] [--input JSON | --input-file F] [--wait [--wait-timeout 5m]]
c1i functions invocations list <function-id> [--page-size N] [--page-token TOKEN] [--limit N]
c1i functions invocations get <invocation-id> --function-id <function-id>
```

Every file-valued flag (`-o/--output`, `--out-dir`, repeatable `--file`, and
`--*-file`) refuses a path containing a control character (exit 2); `-` still
means stdin/stdout.

`functions source` auto-resolves the function's published commit (falling back
to its head/latest draft) and base64-decodes the source files. Without
`--out-dir`, each file is printed to stdout with a `// ===== <name> =====`
delimiter; with `--out-dir`, files are written to disk. Fetched source commonly
inlines credentials, so files are written `0600` and the directory is created,
or tightened if it already exists, to at most `0700` (never widened); a
tightened pre-existing directory prints a warning to stderr naming its old
mode.

`functions usage` scans every automation and emits one row per step that calls
the given function — useful before deleting a draft.

`functions create` and `functions update --file` upload each local file under
its base name (`--file src/main.ts` becomes `main.ts`, the entry file), so
`functions source --out-dir` output uploads back unchanged. The pushed commit
is not published: `functions create` leaves the function unpublished, and
`--file` on update moves only its head. Publish by passing the commit id the
command printed (`.commit.id`) to `functions update <function-id>
--publish-commit`. `functions update` without `--display-name` first reads the
function to resend its current name, which the server validates on every
update; a `--update-mask` that names `displayName` with no value in the object
exits `2` rather than resending it. Other fields go through `--body-file` with
a camelCase `--update-mask`.
`functions delete` is a soft delete: `functions get` still returns the function
with `deletedAt` set, and a second delete exits `4`. The API refuses a function
still referenced by a hook or workflow template until that reference is removed
or retargeted.

`functions invoke` starts an asynchronous run of the published commit (exit `2`
when nothing is published; pass `--commit-id` to run another commit, which is
checked first: one the function lacks exits `4`). `--input` must be a JSON
object. It prints `{"invocationId": ...}`. With `--wait` it then polls until
the run succeeds (exit `0`). Any `--wait` failure exits `1` with an error
saying the invoke "was applied", which only means the run was started: a failed
run is the error naming `ended in FUNCTION_INVOCATION_STATUS_ERROR` (or
`CANCELLED`), and a timeout says so and is not necessarily a failure. Read
the input, output and error with `functions invocations get`.
`functions invocations list` is newest first; a busy function can have tens of
thousands of invocations, so pass `--limit`. The search response carries each
invocation's full input and output, so use a small `--page-size` when payloads
may be large.

`functions list` and `functions commits` read routes whose pagination cursor
stops advancing on large result sets; the guard described under
[Output Conventions](#output-conventions) stops them with the rows already
printed and exit `6`. `functions usage` reads `POST /api/v1/search/automations`,
which pages correctly, and keeps the same guard as a backstop.

### Automations

```sh
c1i automations list [--enabled-only] [--calls-function <fid>] [--page-size N] [--page-token TOKEN] [--limit N]
c1i automations get <automation-id>
c1i automations update <automation-id> [--display-name NAME] [--description TEXT] [--enabled[=false]]
c1i automations update <automation-id> --body-file automation.json --update-mask triggers
c1i automations delete <automation-id>
c1i automations create --display-name NAME [--description TEXT] [--enabled[=false]] [--steps-file steps.json] [--triggers-file triggers.json]
c1i automations create --body-file automation.json
c1i automations execute <automation-id> [--context-file context.json] [--wait [--wait-timeout 4m]]
c1i automations circuit-breaker clear <automation-id> [--decision run|cancel] [--reason TEXT]
c1i automations circuit-breaker resolve-paused <automation-id> --decision run|cancel [--reason TEXT]
c1i automations cancel-pending <automation-id> [--reason TEXT]
c1i automations executions list [--state done|error|pending|...] [--template-id <tid>] [--page-size N] [--page-token TOKEN] [--limit N]
c1i automations executions get <execution-id>
c1i automations executions terminate <execution-id>
```

Each `automations list` row includes `function_ids` (every distinct function the automation invokes), so `--calls-function` can answer "which automations call function X?". `executions list --state` accepts the short forms (`done`, `error`, `pending`, ...) or the full `AUTOMATION_EXECUTION_STATE_*` enum; state and template filtering are applied client-side, so pair a narrow filter with `--limit` to bound the work.

`automations update` sends only the fields you pass (an explicit
`--enabled=false` disables; a blank `--display-name` is refused). For triggers,
steps, draft or circuit-breaker fields, pass an Automation object with
`--body-file` and list the camelCase fields to change in `--update-mask` (see
[Update masks](#update-masks)). Masking `triggers` replaces the published
triggers and increments `currentVersion`; `draftTriggers` and `isDraft` are
left as they were.

`automations delete` is a soft delete (see [Soft delete](#soft-delete)) and
prints `{}`. `automations update` prints the automation unwrapped
(`jq -er .id`), with `webhookCapabilityUrl` and `webhookHmacSecret` as
top-level siblings.

`automations create` takes the common fields as flags: `--steps-file` and
`--triggers-file` are JSON arrays sent as `automationSteps` and `triggers`.
`--body-file` sends a full CreateAutomationRequest verbatim (circuit breaker,
drafts, `appId`, annotations) and excludes every other flag. It prints the
automation unwrapped like `update`. Omitting `--enabled` leaves the server
default, `false`. The API currently returns a 500 for creating an enabled
automation with no steps; create it disabled, add steps, then enable it with
`automations update --enabled`. An automation with no triggers runs only
through `automations execute`, which also starts a disabled automation.

`automations execute` prints `{"executionId": "..."}`; `--context-file` is a
JSON object sent as the execution's context. `--wait` polls
`automations executions get` until the execution ends, printing the id first and
progress on stderr. An execution that ends in `AUTOMATION_EXECUTION_STATE_ERROR`
or `_TERMINATE`, or is paused by the circuit breaker, exits `1` with an error
naming the state and `applied: true` (the execution was started). A
`--wait-timeout` (default 4m) also exits `1`, and the execution may still be
running. `executions get` prints the execution unwrapped (`.state`);
execution ids are integers, and a non-integer one is a `400` (exit `2`).
`executions terminate` prints `{}` and is asynchronous: re-read the
execution, whose state becomes `AUTOMATION_EXECUTION_STATE_TERMINATE`.
Terminating an execution that has already ended (DONE or TERMINATE) exits
`0` and leaves its state unchanged.

`circuit-breaker clear`, `circuit-breaker resolve-paused` and `cancel-pending`
print `{"bulkActionId": "..."}` and are asynchronous: exit `0` means accepted,
and c1i has no command to read a bulk action. The circuit-breaker verbs also
return the deprecated `pausedCount` and `erroredCount`; both were always `0` in
the runs checked, even when paused executions were resolved. A tenant without
the `AUTOMATION_CIRCUIT_BREAKER` feature answers `400` "automation circuit
breaker feature is not enabled for this tenant" (exit `2`). `clear` without `--decision`
only clears the breaker (an empty id). With `--decision`, or on
`resolve-paused` (where it is required), an id came back even with nothing
paused. `--reason` is recorded with the decision. `cancel-pending` cancels queued
executions that have not started. A tenant without that feature answers `400`
`cancelling queued automation runs is not enabled for this tenant` (exit `2`).
An unknown automation id is a `404` (exit `4`) on `execute`, `clear` and
`resolve-paused`.

`automations list` and `automations executions list` read
`POST /api/v1/search/automations` and `POST /api/v1/search/automation_executions`
(the `GET` routes stop paging on large result sets) and keep the stuck-cursor
guard described under Output Conventions as a backstop.

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
c1i policies test-provision <cel>
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
`mcp servers register` uses for its auth config. Only one of the three can
read stdin (`-`); two exit 2 before reading.

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
requires an explicit `--update-mask` (see [Update masks](#update-masks)).

`create`/`update` print the policy unwrapped like `get` (`jq -er .id`); `delete`
prints `{}`. A `--steps-file` or `--body-file` update that states no
`policyType` looks the policy's current type up first (nothing is sent if that
fails); `--dry-run` does the lookup too, and `--policy-type` skips it.

`validate-cel` checks a CEL condition without creating or updating anything.
Its root variable is `subject` (not `user`); this validates the
`rules[].condition` environment specifically, which is NOT the same
environment `ExpressionApproval.expressions` run in (see the command's
`--help`). An invalid condition prints its compile markers and exits 2, so
`c1i policies validate-cel '<cond>' && ...` only continues on a condition
that compiles.

`test-provision <cel>` posts the expression to the account provision policy
tester and prints `{"value", "type"}` (`1+1` gave `"2"`, `number`; nothing is
created). A CEL expression that does not compile is answered with a `500`
carrying the CEL error text (exit `6`), not a `4xx`; c1i does not check CEL
itself.

A soft-deleted policy disappears from `list` and the default `search`; only
`search --include-deleted` finds it there (see [Soft delete](#soft-delete)).
An update to a soft-deleted policy is still accepted (exit `0`); the row stays
deleted but carries the new values.

`list`/`search` rows also carry `step_kinds` (the policy's baseline sequence in
run order, e.g. `["accept"]`) and `baseline_policy_id` — the portable way to
identify a tenant's auto-approval grant policy, since display names vary
(`"Auto-approval"` vs `"Auto approval"`). Match on the shape, not the name —
pipe `c1i policies list` into
`jq -c 'select(.policy_type=="POLICY_TYPE_GRANT" and .system_builtin and .step_kinds==["accept"] and .baseline_policy_id==null)'`.
A tenant can hold more than one such policy, so check what comes back rather
than taking the first row (`rule_count > 0` means conditional rules route to
alternative sequences). A non-null `baseline_policy_id` means the policy defers
its baseline to another — `step_kinds` is then `[]` for a healthy policy.
`policies get` is a verbatim API passthrough and exposes neither derived key;
use `list`/`search` for them.

### MCP

Drive the MCP admin surface (servers, tools, toolsets, and bindings). Most commands take `--app-id`; `mcp servers` commands take the server's `<connector-id>` positionally, while tool/toolset/binding commands scope to a server with `--connector-id`.

Every paginated list below except `mcp gateway list-tools` also takes `--page-token`.

```sh
# Servers (register, configure, and inspect MCP servers)
c1i mcp servers list               --app-id <id> [--page-size N] [--limit N]
c1i mcp servers get                <connector-id> --app-id <id>
c1i mcp servers search             --app-id <id> [--query <text>] [--tool-state approved|pending|disabled|removed] [--include-last-called-at] [--limit N]
c1i mcp servers register           --app-id <id> --type hosted   --display-name <name> --catalog-id <cid> [--source-app-id <id>] [--auth ... ] [--config-field k=v ...] [--user-id <id> ...] [--wait] [--wait-timeout 1m]
c1i mcp servers register           --app-id <id> --type external --display-name <name> --server-url <url> [--transport streamable-http|sse] [--auth ...] [--user-id <id> ...] [--wait] [--wait-timeout 1m]
# --user-id (integration owner) is looked up before sending: an unknown user exits 4, nothing registered
# --wait polls `mcp servers list` until the new server is listed (observed trailing register by a second or two)
c1i mcp servers update             <connector-id> --app-id <id> [--display-name <name>] [--description <text>] [--data-sensitivity ...] [--tool-prefix <p>] [--require-tool-approval]
c1i mcp servers update-credentials <connector-id> --app-id <id> --type hosted|external [--auth ...] [--update-mask <paths>]
c1i mcp servers delete             <connector-id> --app-id <id>
c1i mcp servers resync-tools       <connector-id> --app-id <id>   # EXTERNAL only; 400 on HOSTED
c1i mcp servers test-connection    (--server-url <url> [--transport ...] [--auth ...] | <connector-id> --app-id <id>)   # EXTERNAL only; 400 on HOSTED; exits 8 when unreachable (JSON still printed)
c1i mcp servers discover-oidc      --issuer-url <url>
c1i mcp servers catalog list       [--query <text>] [--page-size N] [--limit N]
c1i mcp servers catalog get        <catalog-id>
c1i mcp servers connections list   [--page-size N] [--limit N]

# Tools
c1i mcp tools list    --app-id <id> --connector-id <id> [--page-size N] [--page-token TOKEN] [--limit N]
c1i mcp tools get     <tool-id> --app-id <id> --connector-id <id>
c1i mcp tools search  --app-id <id> --connector-id <id> [--query <text>] [--state ...] [--classification ...] [--page-size N] [--limit N]
c1i mcp tools approve <tool-id>... --app-id <id> --connector-id <id> [--state approved|disabled|pending]
c1i mcp tools delete  <tool-id> --app-id <id> --connector-id <id>
c1i mcp tools history <tool-id> --app-id <id> --connector-id <id> [--page-size N] [--limit N]

# Resources and prompts (discovered by sync; state is the only writable field, so approve replaces update)
c1i mcp resources list    --app-id <id> --connector-id <id> [--page-size N] [--limit N]   # every state, removed included
c1i mcp resources search  --app-id <id> --connector-id <id> [--query <text>] [--state ...] [--kind static|template] [--include-grant-status] [--limit N]   # removed only with --state removed
c1i mcp resources get     <resource-id> --app-id <id> --connector-id <id>
c1i mcp resources approve <resource-id>... --app-id <id> --connector-id <id> [--state approved|disabled|pending]
c1i mcp resources history <resource-id> --app-id <id> --connector-id <id> [--page-size N] [--limit N]
c1i mcp prompts list    --app-id <id> --connector-id <id> [--page-size N] [--limit N]   # well-formed requests get 403 "feature not enabled" (exit 3) where the tenant lacks prompts
c1i mcp prompts search  --app-id <id> --connector-id <id> [--query <text>] [--state ...] [--include-grant-status] [--limit N]
c1i mcp prompts get     <prompt-id> --app-id <id> --connector-id <id>
c1i mcp prompts approve <prompt-id>... --app-id <id> --connector-id <id> [--state approved|disabled|pending]
c1i mcp prompts history <prompt-id> --app-id <id> --connector-id <id> [--page-size N] [--limit N]

# Toolsets (admin-curated tool groupings; one AppEntitlement per toolset)
c1i mcp toolsets list                   --app-id <id> --connector-id <id> [--page-size N] [--limit N]
c1i mcp toolsets get                    <toolset-id> --app-id <id> --connector-id <id>
c1i mcp toolsets create                 --app-id <id> --connector-id <id> --display-name <name> [--description <text>]
c1i mcp toolsets update                 <toolset-id> --app-id <id> --connector-id <id> --display-name <name> [--description <text>]   # server 400s without --display-name: "DisplayName value length must be between 1 and 255 runes"
c1i mcp toolsets delete                 <toolset-id> --app-id <id> --connector-id <id>
c1i mcp toolsets get-by-entitlement     <app-entitlement-id> --app-id <id>
c1i mcp toolsets requestable-connectors <user-id>   # not paginated; returns the full set in one response

# Bindings (which tools belong to which toolset)
c1i mcp bindings list     --app-id <id> --connector-id <id> --toolset-id <tid> [--page-size N] [--limit N]
c1i mcp bindings create   --app-id <id> --connector-id <id> --toolset-id <tid> --tool-id <id> [--tool-id <id> ...]   # --tool-id max 100
c1i mcp bindings delete   --app-id <id> --connector-id <id> --toolset-id <tid> --tool-id <id> [--tool-id <id> ...]   # --tool-id max 100
c1i mcp bindings by-tools --app-id <id> --connector-id <id> --tool-id <id> [--tool-id <id> ...]   # --tool-id max 32
c1i mcp bindings history  --app-id <id> --connector-id <id> (--toolset-id <tid> | --tool-id <id>) [--page-size N] [--limit N]

# Classifiers (AI governance for MCP gateways)
c1i mcp classifiers list                           [--page-size N] [--page-token TOKEN] [--limit N]
c1i mcp classifiers get                            <classifier-id>
c1i mcp classifiers create                         --body-file classifier.json
c1i mcp classifiers update                         <classifier-id> --body-file classifier.json --update-mask <camelCase-fields>
c1i mcp classifiers delete                         <classifier-id>
c1i mcp classifiers bindings list                  [--page-size N] [--page-token TOKEN] [--limit N]
c1i mcp classifiers bindings create                --body-file binding.json
c1i mcp classifiers bindings delete                <binding-id>
c1i mcp classifiers templates list                 [--latest-only] [--page-size N] [--page-token TOKEN] [--limit N]
c1i mcp classifiers templates get                  <template-id> [--template-version <version>]
c1i mcp classifiers templates instantiate          <template-id> [--body-file params.json]
c1i mcp classifiers templates add-rule             <classifier-id> <template-id> [--body-file params.json]
c1i mcp classifiers policy show
c1i mcp classifiers policy update                  --body-file policy.json --update-mask <camelCase-fields>
c1i mcp classifiers tool-gates list                [--page-size N] [--page-token TOKEN] [--limit N]
c1i mcp classifiers tool-gates search              [--query <text>] [--page-size N] [--page-token TOKEN] [--limit N]
c1i mcp classifiers tool-gates get                 <tool-gate-id>
c1i mcp classifiers tool-gates create              --body-file tool-gate.json
c1i mcp classifiers tool-gates update              <tool-gate-id> --body-file tool-gate.json --update-mask <camelCase-fields>
c1i mcp classifiers tool-gates delete              <tool-gate-id>

# Gateway (verify end to end: list and invoke tools over the live MCP gateway)
c1i mcp gateway list-tools [--full] [--limit N] [--gateway-url <url>]
c1i mcp gateway call <tool-name> [--args '{"k":"v"}'] [--gateway-url <url>]
```

**`mcp classifiers`** manages the AI-governance API introduced for MCP
gateways. Named classifiers are reusable ordered rule cascades; bindings attach
one to an `AGENT` or `GATEWAY` target. `policy` is the separate, tenant-wide
agent policy. A rule with an empty `celCondition` matches every tool call, and
rules execute in order, so read the current object before changing `rules`.

`create` accepts a resource JSON object: `Classifier` for classifiers,
`ClassifierBinding` for bindings, and the full create request for tool gates.
`classifiers update` must include the classifier's required `displayName`,
even when the mask changes another field; read, edit, and resubmit the current
object. Every update requires `--update-mask` (see [Update masks](#update-masks)); include `rules` only when
deliberately replacing the complete cascade. `templates get` shows required
parameter keys. `instantiate` and `add-rule` accept an optional
JSON object with `templateVersion`, `params`, and, for `add-rule`, `ruleIndex`
or `insertIndex`; the positional IDs always select the template and
classifier. A tool gate's `filter` must select either `builtInPattern` or
`celExpression`, not both.

**Auth for `register` / `update-credentials` / `test-connection`:**

- Convenience flags cover the simple methods: `--auth none`,
  `--auth bearer-token --bearer-token TOKEN`,
  `--auth custom-header --header-name NAME --header-value VALUE`,
  `--auth basic-auth --basic-auth-username USER --basic-auth-password PASS`.
  For OAuth2 / AWS SigV4 / Google service-account auth, pass the full config
  object via `--hosted-config-file` / `--external-config-file` (JSON file, or
  `-` for stdin); generate a ready-to-edit skeleton with
  `--print-config-template --auth <method> [--type hosted]` instead of
  hand-writing it.
- A blank companion value exits `2` before anything is previewed or sent, with
  two exceptions. `--token-sharing per-user` leaves the secret to each user
  (`SubmitUserCredential`). On `update-credentials` / `test-connection` an
  explicit `--update-mask` requires only the arm fields whose path it names: a
  coarse path such as `basicAuth` requires both; the rest is carried forward
  from the stored server. A mask must still name the arm itself (or a field in
  it), or it exits `2` as an uncovered config key. `register` has no mask, so it always needs the full arm (a
  custom header's name even under `per-user`).
- `--token-sharing shared|per-user` sets the server's token-sharing mode
  (case-insensitive; `per_user`/`peruser` are also accepted). Per the register
  help, `per-user` is only valid with `oauth2` in authorization-code or
  passthrough mode, `bearerToken`, `customHeader`, or `basicAuth`. Token
  sharing is not readable back, so repeat `--token-sharing per-user` on
  `update-credentials` / `test-connection` when the stored server is per-user
  and you are not supplying the secret.
- On `register` and `update-credentials`, a flag the selected `--type` /
  `--auth` would drop is now a usage error (exit `2`, nothing sent): the other
  type's config file
  (`--hosted-config-file` with `--type external`, `--external-config-file`
  with `--type hosted`), a credential flag with `--auth` unset or
  `--auth none`, and a companion of another method (`--header-name` with
  `--auth bearer-token`). The request body carries one config arm, so a
  dropped value registered a server that differed from what was asked for.
- `test-connection --dry-run` previews the probe and sends nothing.
- Secrets are sealed server-side; reads only ever return `*_configured`
  booleans, never the values. A read-back can legitimately differ from what you
  sent: the backend may store a *resolved* OAuth2 grant such as
  `..._MODE_AUTHORIZATION_CODE` in place of the input mode, so that is a normal
  round-trip, not a bug.
- `--source-app-id` names the source app for a connector-backed HOSTED server.
  `--data-sensitivity`, `--tool-prefix`, `--require-tool-approval` and
  `--user-id` (repeatable; sets the connector's integration owners) can all be
  set at `register` time, not only via `update`.

`mcp tools approve` is the standard post-registration step: newly discovered
tools (from `register` or `resync-tools`) start in `PENDING_REVIEW`, and an
admin approves them for the gateway to proxy calls. It takes one or more tool
ids — the API has no batch approve, so each id is a separate request, but one
invocation covers a whole toolset (pipe `mcp tools search --app-id <id>
--connector-id <id> --state pending --fields id | jq -r .id`). It prints one
compact JSON line per applied tool (`.id`, `.state`), even for a single id; a
failed stdout write aborts the batch, and a partial failure exits with the
first failure's code while earlier ids stay applied (under `--error-format json`
read each per-id error's `applied` and the summary's `notAttempted`).

The other `mcp` mutations print their response unwrapped like the matching
`get` (a server's id is `.connectorId`, a toolset's, classifier's and tool
gate's is `.id`; `mcp classifiers policy update` prints the singleton policy).
Deletes, `resync-tools` and `mcp bindings delete` print `{}`, and
`mcp bindings create` prints `{"bindings":[…]}` as returned.
`mcp servers update-credentials` without `--catalog-id` looks the catalog id up
first and reports it on stderr as `Resolved catalog id: …`, also under
`--dry-run`. History endpoints return records newest-first.

**`mcp gateway`** closes the configure-then-verify loop: after registering a server and approving its tools, `list-tools` runs the MCP handshake against the live gateway and shows what's actually callable, and `call` invokes a tool and prints its result. The gateway URL defaults to the `-mcp` host derived from `--url` (e.g. `acme.conductor.one` → `acme-mcp.conductor.one/v1`); override with `--gateway-url`. Your standard C1 token is accepted by the gateway, so no extra auth is needed. `call` always prints the full result, but exits `7` (not `0`) when the result itself reports `isError: true` — the call succeeded, the tool didn't.

### Access Requests

```sh
c1i requests create grant --app-id <id> --entitlement-id <eid> [--user-id <uid> | --app-user-id <auid>] [--description <text>] [--duration <duration>] [--emergency] [--wait [--wait-min N] [--wait-stable N] [--wait-timeout D]]
c1i requests create revoke --app-id <id> --entitlement-id <eid> [--user-id <uid> | --app-user-id <auid>] [--description <text>]
c1i requests create action --action-id <id> [--description <text>] [--form-values-file <path|->] [--group-only]
c1i requests create resource-action --action-id <id> [--form-values-file <path|->]
c1i requests list [--user-id <id> | --all] [--app-id <id>] [--entitlement-id <id>] [--state open|closed] [--type grant|revoke] [--page-size N] [--page-token TOKEN] [--limit N]
c1i requests get <request-id>
```

On `create`, `--user-id` defaults to the authenticated user when omitted; an
explicit one is looked up first (an unknown user exits `4`, nothing is sent,
`--dry-run` included). `--app-user-id` names one application account instead,
which is how a user with several accounts in the app is targeted; it is looked
up under `--app-id` first (an unknown account exits `4`). Either flag alone
identifies the recipient, and with both the server requires the account to
belong to the user (a mismatch is a 400).
`requests create grant --duration` follows the [Durations](#durations) rule
(`24h`, `86400s`); the server refuses `0s` with `value must be greater than
0s`.

`requests create action` requests a request action (requestable automation)
and `requests create resource-action` runs a connector resource-create action
(for example a group from a template); `--form-values-file` is a JSON object
sent as `formValues`. The API has no route that lists or creates these
actions, so c1i cannot look an id up; an unknown id exits `4` (`action not
found`).
`--group-only` asks the server to refuse unless the action is a current
group-creation template.

`requests create grant --wait` blocks until a new grant for the request shows
up in `grants list` and the matching set stops changing — the same poll
`grants list --wait` runs. The task is printed as soon as it is created
(stdout), while progress goes to stderr. A timeout exits `1` even though the
request was accepted: provisioning takes time, and a request that still needs
approval will not produce the grant until someone approves it. The wait reads
the matching grant set before sending and settles only on a grant that was not
already there, so a recipient who already holds the entitlement waits to the
timeout (the request may still be pending). `--wait-min` defaults to `1` (unlike
`grants list`, where an empty set is a valid answer), and a new grant is always
required, so `0` cannot settle on an empty set;
`--wait-stable` (default `3`) and `--wait-timeout` (default `4m`) match
`grants list`. `--dry-run` never polls.

`requests list` is the requester lens on access requests (the grant/revoke tasks
you file): by default it shows requests you opened or are the subject of —
complementing `tasks list`, which is the approver's My Work lens. Use `--user-id`
to scope to another user or `--all` for every request in the tenant. `--entitlement-id` requires `--app-id`: an entitlement id
repeats across apps, so the pair is the key. `requests
get` fetches a single request (the `.id` printed by `requests create`) as
pretty JSON, including its current policy step and outcome.
`requests create grant`/`revoke` print the created task unwrapped like `get`:
capture it with `jq -er .id`. Its `.state` is a snapshot from creation, not the
outcome.

### Findings

`findings` covers the public REST finding, governance-rule, settings, audit,
and shadow-MCP occurrence surfaces.

```sh
c1i findings search [--body-file <file|->] [--page-size N] [--page-token TOKEN] [--limit N]
c1i findings get <finding-id>
c1i findings create --body-file <file|->
c1i findings state <finding-id> --body-file <file|->
c1i findings assignee <finding-id> --body-file <file|->
c1i findings create-task <finding-id> [--policy-id <policy-id>]
c1i findings bulk-state --body-file <file|->
c1i findings bulk-create-tasks --body-file <file|->

c1i findings routing-rules list [--limit N]
c1i findings routing-rules get <rule-id>
c1i findings routing-rules create --body-file <file|->
c1i findings routing-rules update <rule-id> --body-file <file|->
c1i findings routing-rules delete <rule-id>
c1i findings transformation-rules list [--limit N]
c1i findings transformation-rules get <rule-id>
c1i findings transformation-rules create --body-file <file|->
c1i findings transformation-rules update <rule-id> --body-file <file|->
c1i findings transformation-rules delete <rule-id>
c1i findings settings show
c1i findings settings update --body-file <file|->
c1i findings audits search [--body-file <file|->] [--page-size N] [--page-token TOKEN] [--limit N]
c1i findings shadow-mcp-occurrences search --body-file <file|-> [--page-size N] [--page-token TOKEN] [--limit N]
```

A `--body-file` key that the command itself sets must be absent: any value,
including `null`, exits 2 before any request, and the error lists every
offending key. The one exception is a key set from a positional id, which may
be present when it equals that positional exactly (a string), so a saved `get`
round-trips into `update`; any other value is refused with the key, its value
and the positional named. On `findings search`, `findings audits search` and
`findings shadow-mcp-occurrences search` the keys are `pageToken`, `pageSize`
and their snake_case spellings `page_token` and `page_size`; use `--page-token`
(it fetches that one page) and `--page-size`. Paging keys have no exception.
That command's body must also carry a 27-character `findingId`; a body without
one exits 2 with the server's regex error.
The equality rule covers the id a positional sets: `findingId`/`finding_id` on
`findings state` and `assignee`; `id` on `findings routing-rules update`,
`transformation-rules update`, `automations update`, `access-reviews update`,
`access-reviews templates update`, `policies update`, `mcp classifiers update` and `mcp classifiers tool-gates
update`; and `templateId`/`template_id` on `mcp classifiers templates add-rule`
and `instantiate`.

The rule-list endpoints do not currently expose public filtering or paging; use
`--limit` only to cap local output. Both `list` rows carry `id`, `app_id`,
`display_name`, `description`, `enabled` and `finding_type`; a routing row adds
`priority` (a real integer the API sends), while a transformation row adds
`evaluation_order` (its application sequence, not a rank) and `transform_count`
and has no `priority` — `--fields priority` therefore matches nothing there.
Use JSON files for oneof actions, custom
finding targets, and nested rules; the commands insert positional IDs into the
request. `findings create-task` accepts an optional `--policy-id`, looked up first (no
type check): unknown exits `4`, deleted `2`, and the credential needs policy
read (else `3`), `--dry-run` included; C1 otherwise selects the app policy or
built-in Finding Review policy. `bulk-state` and
`bulk-create-tasks` are asynchronous and return a `bulkActionId`; prefer
explicit `refs` in the JSON to avoid unexpectedly applying a broad
`searchRequest`.

Mutations print the finding or rule unwrapped like `get` (`jq -er .id`);
`routing-rules`/`transformation-rules` `delete` prints `{}`. The exceptions keep
the API's shape: `bulk-state`/`bulk-create-tasks` print `{"bulkActionId"}`,
`settings update` prints `{"list":[…]}`, and `create-task` prints
`{"finding":{…},"taskId":…}`, whose ids are `.taskId` (the created task) and
`.finding.id`.

The public API intentionally excludes its internal summary, app-NHI, and
shadow-MCP notification RPCs. `FindingSearchService.GetSearchTerms` is also not
routed through public REST.

### Settings

`settings` reads and updates tenant-wide singletons. Each has no id: `show`
prints it as pretty JSON, unwrapped from its envelope, and `update` prints the
updated object the same way.

```sh
c1i settings ai-governance show
c1i settings ai-governance update [--default-tool-classification read|write|destructive|sensitive|dangerous] \
  [--audit-verbosity minimal|standard|full] [--auto-discovery[=false]] [--discovery-interval 1h] \
  [--prefer-code-mode[=false]] [--surface-requestable-tools[=false]] [--untrusted-judge-disable[=false]] \
  [--allowed-client-type personal|shared|service|ephemeral ...] \
  [--inactivity-hide-after 24h] [--inactivity-close-after 168h] [--retention-delete-after 2160h]
c1i settings ai-governance history [--limit N]
c1i settings ai-governance tenant-defaults
c1i settings session show
c1i settings session update [--max-session-length 8h] [--enable-idle-timeout[=false]] [--idle-timeout 30m] \
  [--external-clients-enabled[=false]] [--client-id-metadata-document-policy allow-all|requestable|allowlist-only]
c1i settings session test-source-ip --ip <address> [--allow-cidr <cidr> ...]
c1i settings sso show
c1i settings sso update [--enabled[=false]] [--default-subject-type pairwise|public|compatibility] \
  [--default-assertion-lifetime 5m] [--default-id-token-alg eddsa|es256|rs256]
c1i settings sso history [--page-size N] [--page-token TOKEN] [--limit N]
c1i settings cross-app-access show
c1i settings cross-app-access update [--enabled[=false]] [--default-grant-lifetime 1h] \
  [--allow-refresh-token-subjects[=false]] [--default-signing-algorithm eddsa|rs256|es256] \
  [--enabled-signing-algorithm <alg> ...] [--xaa-id-token-lifetime 5m]
c1i settings cross-app-access history [--page-size N] [--page-token TOKEN] [--limit N]
c1i settings contacts show
c1i settings contacts update [--security-email <email> ...] [--billing-email <email> ...] [--operations-email <email> ...]
c1i settings request show
c1i settings request update [--skip-justification[=false]] [--max-bulk-entitlement-selection N]
c1i settings domains list [--page-size N] [--page-token TOKEN] [--limit N]
c1i settings domains set --domain <domain> ... | --body-file <file|->
c1i settings notifications org show
c1i settings notifications org update --body-file <file|->
c1i settings notifications user show
c1i settings notifications user update --body-file <file|->
c1i settings aws-external-id show
c1i findings settings edge show
c1i findings settings edge update --body-file <file|->
```

- **Masked updates** (`ai-governance`, `session`, `sso`, `cross-app-access`,
  `contacts`, `request`) derive the mask from the flags you pass; anything else
  goes through `--body-file` (the object `show` prints) with an explicit
  `--update-mask`, as in [Update masks](#update-masks). To clear a field, mask
  it and leave it out of the body: `{}` with `--update-mask
  defaultGrantLifetime` sets that lifetime back to `null`. A repeatable list
  flag (`--allowed-client-type`, `--enabled-signing-algorithm`, the contact
  email flags) replaces the whole list. The ai-governance lifecycle flags
  change one `defaultClientLifecycle` duration each and leave the other two.
- The server bounds some values and says so when it refuses one (exit `2`):
  `--max-session-length` must be one of the lengths it lists, and
  `--max-bulk-entitlement-selection` must be in `[0, 50]`. Contact addresses
  must be valid email addresses. `sso update` on a tenant without the SSO
  provider feature is a 403, `feature not enabled` (exit `3`).
- `ai-governance history` is one read (the endpoint takes no paging); the `sso`
  and `cross-app-access` histories page like any list. Each record is the
  change metadata plus the full settings snapshot, as returned.
- `session test-source-ip` asks whether `--ip` passes the `--allow-cidr` rules
  you give it; it changes nothing and exits `0` for either answer (read
  `.allowed`). With no `--allow-cidr` the answer is `false`. The source-IP
  allowlists themselves have no flags: send them with `--body-file` and
  `--update-mask`, and test first, since a list that leaves out your address
  can lock you out.
- `domains set` replaces the whole set with exactly the `--domain` values; list
  first and pass every domain you keep. `--body-file '{"newDomains": []}'`
  empties it; a body without a `newDomains` array exits `2`, as does an element
  that is not a string or is blank. The server does not check that a value
  looks like a domain.
- `notifications org|user update` sends `--body-file` as the whole request
  (`{"channelSettings": {...}}`, what `show` prints); there is no mask, so a
  body without a non-empty `channelSettings` object exits `2` and nothing is
  sent. In the run checked, a partial body on the user scope changed only the
  settings it named. The org scope's partial-body behavior is unmeasured: for
  org, send back the whole `show` output with your edits. Sending settings
  back can turn an event `show` printed as `null` (such as `system`) into its
  default.
- `findings settings edge update` takes `{"settings": [{"category": ...,
  "enabled": ...}], "subcategorySettings": [...]}` and prints the resulting
  `{"list", "subcategories"}`. A body with neither list non-empty exits `2`
  and nothing is sent.

Not covered: the tenant email provider, identity-policy defaults, onboarding
state and per-user developer preferences (use `c1i api`).

### Cross-app access (XAA)

`xaa` manages an app's cross-app access objects: resource servers
(third-party authorization servers C1 mints grants for), their OAuth scopes,
access profiles (requestable bundles of one server's scopes) and the scopes
bound to each profile, plus each resource server's client audience mappings.
Resource servers, scopes and access profiles take `--app-id`; client audience
mappings take `--resource-server-id` instead, since their routes carry no app.
The tenant-wide switch is `settings cross-app-access` ([Settings](#settings)).

```sh
c1i xaa resource-servers list --app-id <app-id> [--page-size N] [--page-token TOKEN] [--limit N]
c1i xaa resource-servers search [--app-id <id> ...] [--query <text>] \
  [--signing-algorithm unspecified|eddsa|rs256|es256 ...] [--disabled[=false]] [--limit N]
c1i xaa resource-servers get <resource-server-id> --app-id <app-id>
c1i xaa resource-servers create --app-id <app-id> --audience <https-issuer> --display-name <name> \
  [--description <text>] [--resource-uri <uri> ...] [--max-grant-lifetime 1h] \
  [--signing-algorithm eddsa|rs256|es256] [--require-proof-of-possession] [--disabled] | --body-file <file|->
c1i xaa resource-servers update <resource-server-id> --app-id <app-id> [--display-name <name>] [--description <text>] \
  [--resource-uri <uri> ...] [--max-grant-lifetime 1h] [--signing-algorithm <alg>] \
  [--require-proof-of-possession[=false]] [--disabled[=false]] | --body-file <file|-> --update-mask <paths>
c1i xaa resource-servers delete <resource-server-id> --app-id <app-id>
c1i xaa resource-servers history <resource-server-id> --app-id <app-id> [--limit N]
c1i xaa resource-servers resync-scopes <resource-server-id> --app-id <app-id>
c1i xaa scopes list --app-id <app-id> [--limit N]
c1i xaa scopes get <scope-id> --app-id <app-id>
c1i xaa scopes delete <scope-id> --app-id <app-id>
c1i xaa scopes history <scope-id> --app-id <app-id> [--limit N]
c1i xaa scopes create --app-id <app-id> --resource-server-id <id> --scope-value <scope> --display-name <name> \
  [--description <text>] [--classification read|write|destructive|sensitive|dangerous] \
  [--state pending-review|enabled|disabled]
c1i xaa scopes update <scope-id> --app-id <app-id> [--display-name <name>] [--description <text>] \
  [--state pending-review|enabled|disabled] [--classification <class>]
c1i xaa access-profiles list --app-id <app-id> [--limit N]
c1i xaa access-profiles get <access-profile-id> --app-id <app-id>
c1i xaa access-profiles delete <access-profile-id> --app-id <app-id>
c1i xaa access-profiles history <access-profile-id> --app-id <app-id> [--limit N]
c1i xaa access-profiles for-entitlement <entitlement-id> --app-id <app-id>
c1i xaa access-profiles create --app-id <app-id> --resource-server-id <id> --display-name <name> [--description <text>]
c1i xaa access-profiles update <access-profile-id> --app-id <app-id> [--display-name <name>] [--description <text>]
c1i xaa access-profiles scopes <access-profile-id> --app-id <app-id> [--limit N]
c1i xaa access-profiles add-scopes <access-profile-id> --app-id <app-id> --scope-id <scope-id> [--scope-id <scope-id> ...]
c1i xaa access-profiles remove-scopes <access-profile-id> --app-id <app-id> --scope-id <scope-id> [--scope-id <scope-id> ...]
c1i xaa client-audience-mappings list --resource-server-id <id> [--limit N]
c1i xaa client-audience-mappings search [--resource-server-id <id> ...] [--query <text>] [--disabled[=false]] [--limit N]
c1i xaa client-audience-mappings get <client-key> --resource-server-id <id>
c1i xaa client-audience-mappings create --resource-server-id <id> --client-key <client-key> \
  --audience-client-id <client-id> [--disabled]
c1i xaa client-audience-mappings update <client-key> --resource-server-id <id> \
  [--audience-client-id <client-id>] [--disabled[=false]]
c1i xaa client-audience-mappings delete <client-key> --resource-server-id <id>
c1i xaa client-audience-mappings history <client-key> --resource-server-id <id> [--limit N]
```

- `--audience` must be https; the server stores it normalized (lowercase host,
  no trailing slash) and it is immutable. It is unique across the tenant (409,
  exit `2`) and must not be the tenant's own issuer. Registering a server
  starts a scope discovery run; `get` shows its outcome as
  `scopeDiscoveryStatus`, and `resync-scopes` starts another.
- All three creates look up `--app-id` and (for scopes and access profiles)
  the resource server first, because the server stores an object under an
  unknown or deleted one: unknown exits `4`, deleted exits `2`, `--dry-run`
  included.
- Updates fill the fields the server validates on every update (display name,
  audience, scope value, resource server id) from a read when you leave them
  unset; the mask still decides what changes. A scope is approved with
  `scopes update --state enabled`; `--state removed` exits `2` (create refuses
  it server-side). The resource server's `modifyClaimsHook` and clearing a
  field go through `--body-file` with `--update-mask` (camelCase paths; the
  server refused `*` and snake_case paths with exit `2`). A body key counts in
  either spelling (`display_name` or `displayName`). A hook's `functionId`, and
  `commitId` if given, are looked up first: unknown exits `4`, since the server
  stores either. An update of a deleted scope or resource server exits `0` and
  applies; `deletedAt` stays set.
- `search --disabled=true` lists only disabled servers and `--disabled=false`
  only enabled ones; omit it for both. In the run checked, `--query` matched
  display names, not audiences, and a just-created server took a few seconds
  to appear. Scope, access-profile and scope-binding search are not
  implemented server-side (501); use `list --app-id` or `access-profiles
  scopes`.
- `access-profiles add-scopes` and `remove-scopes` take a repeatable
  `--scope-id` (a repeat is sent once; the server takes at most 100). Each
  scope is looked up first under `--app-id`: unknown exits `4` (the server
  answers 400 on add and a silent success on remove), and a deleted one exits
  `2` on add. A scope of another resource server exits `2` from the server.
  Re-adding a bound scope, or removing an unbound one, exits `0`. `add-scopes`
  prints `{"bindings": [...]}`; `remove-scopes` prints `{}`.
- A client audience mapping's id is its client key, which may be a URL; c1i
  sends it as a query parameter or in the body, never in the path. The server
  accepted a CIMD client id URL and a `dcr://` key, and refused other forms
  ("client_key is not a recognized form"). `create` looks the resource server
  up first with a tenant-wide resource-server search, because the server
  stores a mapping under an unknown resource server: a server the search does
  not return exits `4`, `--dry-run` included. Search omits deleted servers,
  and a server created a moment earlier was missing from it for about a second
  in the run checked; retry then. A key already mapped exits `2`, and so does
  a key whose mapping was deleted: the server refuses to recreate it. `update`
  fills the audience client id from a read when you leave it unset (the
  server validates it whatever the mask names), so an unknown key exits `4`
  before the update. Deletes are soft; `get` still returns the mapping with
  `deletedAt`, and `update` still changes it.
- Deletes are soft: `get` returns the object with `deletedAt`, `list` stops
  listing it, and repeating the delete exits `0`. Deleting a resource server
  deleted its scopes and access profiles, and each scope or profile's backing
  entitlement went with it within seconds, in the run checked.
- `history` and `list` of an unknown id or app list nothing and exit `0`.
- `access-profiles for-entitlement` exits `4` for an entitlement with no
  profile; in the run checked it also did so for a profile created seconds
  earlier.

Not covered yet: access-profile scope bindings and client-audience mappings
(use `c1i api`).

### Role mining

`role-mining` manages the public role-mining analysis workflow, its suggested
access profiles, and tenant configuration.

```sh
c1i role-mining trigger
c1i role-mining suggestions list [--limit N]
c1i role-mining suggestions search --body-file <file|-> [--page-size N] [--page-token TOKEN] [--limit N]
c1i role-mining suggestions get <suggestion-id>
c1i role-mining suggestions state <suggestion-id> --body-file <file|->
c1i role-mining suggestions users <suggestion-id> --body-file <file|-> [--page-size N] [--page-token TOKEN] [--limit N]
c1i role-mining config show
c1i role-mining config update --body-file <file|->
c1i role-mining runs list [--limit N]
c1i role-mining runs latest
c1i role-mining custom-analysis list [--limit N]
c1i role-mining custom-analysis latest
c1i role-mining custom-analysis get <analysis-id>
c1i role-mining custom-analysis trigger --body-file <file|->
c1i role-mining custom-analysis evaluate <analysis-id> --body-file <file|->
c1i role-mining access-profiles create --body-file <file|->
```

The `--body-file` owned-key rule under [Findings](#findings) applies here too. On
`role-mining suggestions search` and `role-mining suggestions users` the keys
are `pageToken`, `pageSize`, `page_token` and `page_size`; use `--page-token`
and `--page-size`. `users` also refuses `suggestionId`/`suggestion_id` unless
the value equals the `<suggestion-id>` argument exactly.

`trigger` queues an organization analysis and returns an enqueue acknowledgement;
inspect `runs latest` for its eventual result. `custom-analysis trigger` returns
an analysis ID that `custom-analysis get` can poll. The latest custom-analysis
pointer is scoped to the authenticated user; custom-analysis list and get are
tenant-scoped. Mutations print the API's response: `suggestions state` the
suggestion (`.id`), `config update` the configuration (unwrapped, as
`config show` prints it), and `trigger` `{"runId"}`; `custom-analysis trigger`
prints `{"id"}` and `access-profiles create` `{"accessProfileId","celExpression"}`.
`config update` replaces the entire configuration, so begin with
`config show`. Access-profile creation persists the profile, entitlement bindings,
and optional automation as separate service operations; validate its JSON before
submitting it.

The suggestions, runs, and custom-analysis list routes do not accept paging or
state filters through public REST, despite returning a continuation token.
`--limit` only caps local output. Organization overview,
legacy cohort analysis, coverage streaming, and the core MCP role-mining
service are not public REST routes.

### Access-review campaigns

Campaigns certify whether existing access should continue. The API calls the
resource an `access_review`, but c1i uses the UI's `access-reviews` name.

```sh
c1i access-reviews list [--page-size N] [--page-token TOKEN] [--limit N]
c1i access-reviews get <campaign-id>
c1i access-reviews create --body-file <file|->
c1i access-reviews update <campaign-id> --body-file <file|-> --update-mask <paths>
c1i access-reviews delete <campaign-id>
c1i access-reviews scope get <campaign-id>
c1i access-reviews scope set <campaign-id> --body-file <file|->
c1i access-reviews scope set-excluded <campaign-id> --body-file <file|->
c1i access-reviews scope set-by-resource-type <campaign-id> --body-file <file|->
c1i access-reviews entitlements add <campaign-id> --app-id <app-id> --entitlement-id <id>...
c1i access-reviews entitlements remove <campaign-id> --app-id <app-id> --entitlement-id <id>...
c1i access-reviews excluded-entitlements add <campaign-id> --app-id <app-id> --entitlement-id <id>...
c1i access-reviews excluded-entitlements remove <campaign-id> --app-id <app-id> --entitlement-id <id>...
c1i access-reviews reports list <campaign-id> [--page-size N] [--page-token TOKEN] [--limit N]
c1i access-reviews reports generate <campaign-id> [--format json|csv|xlsx]
c1i access-reviews reports generate <campaign-id> --body-file <file|->
```

`create` takes the complete API request object. Its `scopeV2` is a deeply
nested oneof, so the CLI intentionally accepts it through `--body-file` rather
than a lossy flag surface; at least one `ownerIds` entry and a 27-character
`policyId` are required by the server. `update` takes a partial `AccessReview`
object, inserts the id, and wraps it with the required explicit `--update-mask`
(see [Update masks](#update-masks)). Prepared campaigns cannot change their
scope or policy. `create` and `update` print the campaign unwrapped like `get`
(`jq -er .id`); `reports generate` prints `{}`.

`delete` soft-deletes: the campaign leaves `list`, but `get` still returns it
with `state` `ACCESS_REVIEW_STATE_DELETED` (the model has no `deletedAt` field;
check `state`), and deleting it again exits `0`. The API says the
delete also takes the campaign's "dependent objects" with it. Prints `{}`.

`scope get` prints a campaign's `scopeV2`, its selected entitlements (`list`)
and its exclusions in one object; it is not paginated. The three `scope set*`
commands replace the scope and the whole selection, excluded set, or
resource-type selection from `--body-file`. To change part of a selection, use
`entitlements add|remove` or `excluded-entitlements add|remove`. The first pair
works only while `scopeV2` sets `specificResources`, which `add` sets on a
campaign with no scope yet; the server takes 1 to 200 per `add`, skips
already-selected entitlements and ignores unselected ones. Exclusions apply only while `scopeV2` sets `excludedSpecificResources`: set it
with `scope set-excluded` first. In the run checked, removing the last excluded
entitlement also cleared it. The API's `scope_by_excluded_resource_type` route
is "superseded and scheduled for removal", so it has no command; exclude by
resource type with `scope set-excluded` and
`scopeV2.excludedAppSelectionCriteria`. Every command that names an
entitlement or resource type looks each one up first, so an unknown one exits
`4` and a soft-deleted one `2`, before anything is sent, `--dry-run` included;
the server alone would answer `400`, `500` or a silent skip.

#### Access-review templates

A template is a reusable campaign configuration: owners, review policy, scope
and schedule. The API has no route that lists or searches templates; an id
comes from `templates create` (`.id`) or from a campaign made from a template
(`access-reviews list` rows carry `accessReviewTemplateId`).

```sh
c1i access-reviews templates get <template-id>
c1i access-reviews templates create --display-name <name> --policy-id <policy-id> --owner-id <user-id>... \
  [--description <text>] [--review-instructions <text>] [--scope-type by-entitlements|by-users|...]
c1i access-reviews templates create --body-file <file|->
c1i access-reviews templates update <template-id> [--display-name <name>] [--description <text>] \
  [--review-instructions <text>] [--policy-id <policy-id>] [--scope-type <type>]
c1i access-reviews templates update <template-id> --body-file <file|-> --update-mask <paths>
c1i access-reviews templates delete <template-id>
c1i access-reviews templates scope get <template-id>
c1i access-reviews templates scope set <template-id> --body-file <file|->
c1i access-reviews templates scope set-excluded <template-id> --body-file <file|->
c1i access-reviews templates scope set-by-resource-type <template-id> --body-file <file|->
c1i access-reviews templates entitlements add <template-id> --app-id <app-id> --entitlement-id <id>...
c1i access-reviews templates entitlements remove <template-id> --app-id <app-id> --entitlement-id <id>...
c1i access-reviews templates excluded-entitlements add <template-id> --app-id <app-id> --entitlement-id <id>...
c1i access-reviews templates excluded-entitlements remove <template-id> --app-id <app-id> --entitlement-id <id>...
```

The server requires a display name, a review policy and 1 to 10 owners. The
flags create a template with no scope and no schedule; schedule and recurrence
fields (`isCampaignScheduleEnabled`, `autoStartCampaign`, `recurrenceRule`, ...)
have no flags and go through `--body-file`, which is mutually exclusive with the
other flags. Every owner id and the policy id, from flags or a body file, are
looked up first, before anything is sent, `--dry-run` included: an unknown one
exits `4`. A policy that exists but is deleted or not a certify policy exits
`2`. The server alone stores an unknown owner id and a policy of any type
without complaint.

`update` derives its mask from the flags; `--body-file` with `--update-mask`
covers every other field. The server validates `displayName` and `policyId` on
every update whatever the mask names, so when the request leaves either unset,
`update` reads the template first and fills it in (the mask still decides what
changes); that read makes an unknown `<template-id>` exit `4`. A `policyId` is
looked up only when the mask names it. A `--body-file` mask that names
`displayName` or `policyId` needs that field in the file, or `update` exits `2`
before sending anything. `create` and `update` print
the template unwrapped like `get` (`jq -er .id`).

`delete` prints `{}`. The API says a deleted template "can no longer be used to
create campaigns". Exit `0` only means the API accepted the request; in the
run checked, the template stayed readable with `deletedAt` null, and deleting
it again exited `0`.

The scope commands work like the campaign ones above, with the template's
scope under `scope` instead of `scopeV2`, and the same lookups of every
referenced entitlement and resource type.

Report generation is asynchronous: `generate` creates a report record but does
not return a ready download. Use `reports list` to find its state and
time-limited `downloadUrl`. Public API callers default to JSON when `--format`
is omitted; use `csv` or `xlsx` when needed. `--body-file` is for the full
generation request, including XLSX report-column configuration; it is mutually
exclusive with `--format`.

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
c1i access-profiles create --display-name <name> [--description <text>] [--published] [--visible-to-everyone] [--request-bundle] [--enrollment-behavior bypass|enforce] [--unenrollment-behavior leave-access-as-is|revoke-all|revoke-unjustified] [--unenrollment-entitlement-behavior bypass|enforce]
c1i access-profiles update <access-profile-id> [--display-name <name>] [--description <text>] [--published] [--visible-to-everyone] [--request-bundle] [--enrollment-behavior bypass|enforce] [--unenrollment-behavior leave-access-as-is|revoke-all|revoke-unjustified] [--unenrollment-entitlement-behavior bypass|enforce]
c1i access-profiles delete <access-profile-id>

c1i access-profiles requestable-entitlements list <access-profile-id> [--page-size N] [--page-token TOKEN] [--limit N]
c1i access-profiles requestable-entitlements list-ids <access-profile-id>
c1i access-profiles requestable-entitlements add <access-profile-id> --app-id <id> --entitlement-id <id> [--entitlement-id <id> ...] [--create-requests]
c1i access-profiles requestable-entitlements remove <access-profile-id> --app-id <id> --entitlement-id <id> [--entitlement-id <id> ...]
c1i access-profiles requestable-entitlements set <access-profile-id> --refs-file <path>

c1i access-profiles visibility-entitlements list <access-profile-id> [--page-size N] [--page-token TOKEN] [--limit N]
c1i access-profiles visibility-entitlements add <access-profile-id> --app-id <id> --entitlement-id <id> [--entitlement-id <id> ...]
c1i access-profiles visibility-entitlements remove <access-profile-id> --app-id <id> --entitlement-id <id> [--entitlement-id <id> ...]

c1i access-profiles bundle-automation get <access-profile-id>
c1i access-profiles bundle-automation create <access-profile-id> --body-file <path>
c1i access-profiles bundle-automation set <access-profile-id> --body-file <path>
c1i access-profiles bundle-automation delete <access-profile-id>
c1i access-profiles bundle-automation resume <access-profile-id>
c1i access-profiles bundle-automation run <access-profile-id> [--refs-file <path>]
```

`access-profiles create` needs only `--display-name`. Every other field is
omitted unless explicitly passed; explicit `false` still reaches the API.
`update` derives its field mask from the flags supplied, so an explicit
`--description ""` clears it while omitted fields remain untouched. Behavior
flags accept short values shown above or the full `REQUEST_CATALOG_*` API enum.

`delete` is a soft delete (see [Soft delete](#soft-delete)).

`create` and `update` print the profile unwrapped like `get` (`jq -er .id`;
the count is `.memberCount`). `bundle-automation create`/`set` print the
automation (id path `.requestCatalogId`); every delete, `resume`, `run` and
`requestable-entitlements`/`visibility-entitlements` mutation prints `{}`.

`requestable-entitlements` manages what the profile grants. `add` and `remove`
scope each entitlement to one `--app-id`; repeat `--entitlement-id` for that
application. `set` takes a JSON array of `{"appId":"…","id":"…"}` via
`--refs-file` (or `-` for stdin) and replaces the profile's entire requestable
set; use `[]` to clear it. `list-ids` returns the API's compact `{appId,id}`
reference list. `--create-requests` asks the server to create requests for
profile members when adding entries.

`visibility-entitlements` controls who can see the profile. Visibility additions
require a catalog that is published but not visible to everyone. The API returns
`catalog must be published to add an access entitlement` for an unpublished
catalog and `catalog is visible to everyone, cannot add access entitlements`
when `--visible-to-everyone` is set. Measured caveat: in one run both a
requestable and a visibility add on an unpublished catalog returned `200 {}` and
the binding was stored (it is not effective until the catalog is published);
publish first and verify with `access-profiles get`.
`--enrollment-behavior unspecified` resets to the server default (measured:
ENFORCE becomes BYPASS), and `--unenrollment-behavior unspecified` resets to
LEAVE_ACCESS_AS_IS; the reset value for
`--unenrollment-entitlement-behavior` was not measured.

`bundle-automation` drives automatic bundled-entitlement grants. `create` and
`set` take a JSON object via `--body-file` (or `-` for stdin). The CLI validates
and sends `createTasks`, `disableCircuitBreaker`, `enabled`, and `entitlements`,
which contains an `entitlementRefs` array of `{"appId":"…","id":"…"}` objects.
`entitlements` is required even to disable the automation: the server refuses a
body without conditions, and an empty `entitlementRefs` list is how you say
"none" (exit 2 client-side if it is missing). `run --refs-file` takes that
entitlement reference array and limits a run to those references.

`access-profiles list` rows do **not** carry a member count: the list endpoint reports
`memberCount` as `0` for every catalog while `access-profiles get` on the same id
reports a non-zero count, so the key is omitted from list rows. `access-profiles get`
also carries the catalog's `accessEntitlements` (its visibility bindings),
empty when there are none, which list rows omit.

### Roles

Role ids are what `--scoped-role` takes on `service-principals credentials
create` and `service-principals trusts create|update`; `auth login
--scoped-role` also takes a role name (see [Authentication](#authentication)).

```sh
c1i roles list [--page-size <n>] [--page-token <token>] [--limit <n>]
c1i roles get <role-id>
```

List rows carry `id`, `name` (e.g. `system:viewer`), `display_name`,
`system_builtin`, `system_api_only`, `created_at` and `updated_at`; `get` adds
the role's `serviceRoles` and `permissions`. A role id is 27 letters and
digits; a malformed one is rejected with `value does not match regex pattern
"^[a-zA-Z0-9]{27}$"` (exit 2).

### Service principals

A service principal (alias `sp`) is a tenant-owned non-human identity. The principal is
just an identity; a **client credential** on it is what a caller authenticates
with (a `client_id`/secret pair), and a **binding** lets another subject — a
function, SSO application, AuthZEN server, or edge — act as the principal.

These first-class commands are the way to reach it; the raw `c1i api` escape
hatch works too.

```sh
c1i service-principals list [--page-size <n>] [--page-token <token>] [--limit <n>]
c1i service-principals get <sp-id>
c1i service-principals create --display-name <name>
c1i service-principals update <sp-id> --display-name <name>
c1i service-principals delete <sp-id>
```

`service-principals` is aliased to `sp`. `create` needs only `--display-name`;
the new principal is printed as pretty JSON, unwrapped like
`service-principals get`; read its id with `jq -er .id`. `delete` prints `{}`.

**Delete is destructive:** `service-principals delete` removes the principal,
every credential issued for it, and its workload federation trusts. Before deleting, inspect the principal with
`service-principals get <sp-id>`. Use `--dry-run` to preview the DELETE request
and target path; it does not verify that the target exists.

**Credentials** are how a service principal authenticates. The secret is returned
**once**, at creation, and cannot be retrieved again — capture it then.
`credentials create` prints the credential unwrapped, with the secret as a
sibling: `jq -er .id`, `.clientId` and `.clientSecret`. If writing that output
fails (a full disk, a closed pipe), the command exits `1` with `applied: true`
and says to revoke the credential and create a new one. `credentials update`
prints the credential; `revoke`, `bindings add` and `bindings delete` print `{}`.

```sh
c1i service-principals credentials list <sp-id>
c1i service-principals credentials get <credential-id> --service-principal-id <sp-id>
c1i service-principals credentials create <sp-id> --display-name <name> [--expires <duration>] [--scoped-role <role-id>] [--allow-cidr <cidr>] [--require-dpop]
c1i service-principals credentials update <credential-id> --service-principal-id <sp-id> --display-name <name>
c1i service-principals credentials revoke <credential-id> --service-principal-id <sp-id>
```

`service-principals delete` and `credentials revoke` read back differently; see
[Soft delete](#soft-delete).

`--expires` follows the [Durations](#durations) rule. The server accepts the
range `(0s, 4320h]` — up to 180 days — rejecting `0s` or more with `value must
be inside range (0s, 4320h0m0s]`.

`--scoped-role` and `--allow-cidr` are repeatable and restrict the credential to
those role ids / source CIDRs. Repeat either flag for multiple values. An empty
occurrence is a usage error before any request is sent. `--require-dpop` requires
DPoP proof-of-possession at token exchange. Only `--display-name` can be changed
after a credential is created.

**Bindings** (a draft API) name a subject that authenticates as the principal.
Exactly one subject is required; the SSO, AuthZEN, and edge subjects are
app-scoped, so they take both an id and an app id.

```sh
c1i service-principals bindings add --service-principal-id <sp-id> --function-id <fn-id>
c1i service-principals bindings add --service-principal-id <sp-id> --sso-application-id <id> --sso-app-id <app-id>
c1i service-principals bindings add --service-principal-id <sp-id> --authzen-server-id <id> --authzen-app-id <app-id>
c1i service-principals bindings add --service-principal-id <sp-id> --edge-id <id> --edge-app-id <app-id>
c1i service-principals bindings list --function-id <fn-id>
c1i service-principals bindings delete --service-principal-id <sp-id> --function-id <fn-id>
```

`bindings list` is by subject — it returns the principals a subject is bound to.
`add` and `delete` are idempotent. `bindings list` and `delete` take the same
subject flags as `add` (exactly one subject).

### Workload federation

Workload federation lets an external workload (a CI job, a SPIFFE workload)
exchange its own JWT for a token that acts as a service principal. A
**provider** registers the JWT issuer for the tenant; a **trust** on a service
principal names a provider and a CEL condition over the token's claims.

```sh
c1i workload-federation providers list [--page-size <n>] [--page-token <token>] [--limit <n>]
c1i workload-federation providers get <provider-id>
c1i workload-federation providers create --display-name <name> --well-known-provider <kind> --issuer-url <url> [--bundle-endpoint-url <jwks-url>] [--description <text>]
c1i workload-federation providers update <provider-id> [--display-name <name>] [--description <text>] [--disabled[=false]] [--bundle-endpoint-url <jwks-url>]
c1i workload-federation providers delete <provider-id>
c1i workload-federation test-cel --expression <cel> (--claims <json> | --claims-file <path|->)
```

`workload-federation` is aliased to `wf`. `--well-known-provider` is `custom`,
`github-actions`, `gitlab-ci`, `hcp-terraform`, `aws-iam-outbound` or `spiffe`.
`spiffe` takes a `spiffe://` trust-domain issuer and requires
`--bundle-endpoint-url`, the HTTPS JWKS URL of its signing keys; every other
kind takes an `https://` issuer, which the API checks by OIDC discovery, and
refuses `--bundle-endpoint-url` (exit `2`, nothing sent). The API refuses an
issuer it cannot reach, one that does not fit a named kind, and one already
registered in the tenant (409, exit `2`); the kind and issuer are fixed after
creation. `providers update --bundle-endpoint-url` on a non-SPIFFE provider
answers 200 and changes nothing. `providers delete` is refused while a trust
references the provider (find them with `trusts search --provider-id`, which
can lag a just-created trust by about a second); `providers update` on a
deleted provider answers 200 and applies, and the provider stays out of
`providers list`. Rows carry `id`, `display_name`, `description`,
`issuer_url`, `well_known_provider`, `disabled`, `created_at`, `updated_at`,
and `bundle_endpoint_url` on a SPIFFE provider.

`test-cel` evaluates an expression against sample claims without a token,
provider or trust, prints `matched`, `error` and `expression`, then exits `0`
when it matched, `1` when it evaluated to false, and `2` when the API reports
an error (a syntax error, a non-bool result, claims that are neither JSON nor
a JWT). Pass a JWT with `--claims-file`, not `--claims`, to keep it off the
command line.

```sh
c1i service-principals trusts list <sp-id> [--page-size <n>] [--page-token <token>] [--limit <n>]
c1i service-principals trusts search [--service-principal-id <sp-id>] [--provider-id <provider-id>] [--query <text>]
c1i service-principals trusts get <trust-id> --service-principal-id <sp-id>
c1i service-principals trusts create <sp-id> --provider-id <provider-id> --display-name <name> --condition <cel> [--description <text>] [--scoped-role <role-id>] [--allow-cidr <cidr>] [--passthrough-claim <claim>]
c1i service-principals trusts update <trust-id> --service-principal-id <sp-id> [--display-name <name>] [--description <text>] [--condition <cel>] [--disabled[=false]] [--scoped-role <role-id>] [--allow-cidr <cidr>] [--passthrough-claim <claim>]
c1i service-principals trusts update <trust-id> --service-principal-id <sp-id> --body-file <path|-> --update-mask <paths>
c1i service-principals trusts delete <trust-id> --service-principal-id <sp-id>
c1i service-principals trusts test-token <trust-id> --service-principal-id <sp-id> --subject-token-file <path|-> [--source-ip <ip>]
```

A trust's id is its client id (`word-word-12345@tenant.example.com/wfe`, as
rows print it in `client_id`); `<trust-id>` takes that or the part before
`@`, which is all c1i sends: the rest is not checked, and the trust is looked
up under `--service-principal-id`. `--condition` is a CEL expression over `claims`
that must return a bool; the API refuses an invalid one (exit `2`). The API
stores a trust for an unknown service principal or `--scoped-role`, so
`create` looks up `<sp-id>` and each `--scoped-role` first, and `update` each
`--scoped-role`: an unknown one exits `4` before anything is sent,
`--dry-run` included. A human user's id as `<sp-id>` is an API 500 (exit
`6`). The API documents a trust's effective permissions as "min(SP roles,
trust.scoped_role_ids)". `--scoped-role`, `--allow-cidr` and
`--passthrough-claim` are repeatable and replace the whole list on `update`;
to empty one, use `--body-file` with `--update-mask`, e.g.
`{"scopedRoleIds":[]}` and `--update-mask scopedRoleIds`. A mask naming a list
the body leaves out or sets to `null` exits `2` unsent, since the API would
clear it (an empty `scopedRoleIds` gives the trust all the principal's roles);
role ids in the body, in either spelling, are looked up like `--scoped-role`. An unknown
`<sp-id>` on `list`, or an unknown `--service-principal-id` or
`--provider-id` on `search`, lists nothing and exits `0`.

`test-token` checks a workload JWT against a trust without issuing a token.
The JWT comes only from `--subject-token-file` (`-` for stdin). It prints the
API's per-step report (`overallResult`, `decodedClaimsJson`, `jwtDecode`,
`issuerMatch`, `signatureValidation`, ...), then exits `0` when `overallResult`
is true and `1`, naming the failed steps, when it is false. A disabled trust or
provider is an API 500 (exit `6`).

### User attribute mappings

A mapping copies a value from an app's synced users into a structured C1.ai
user field: employment type, department, job title, manager and so on. Without
one, those fields stay empty however rich the app's profile data is.

```sh
c1i attribute-types list [--limit <n>]
c1i attribute-mappings list [--app-id <app-id>] [--page-size <n>] [--page-token <token>] [--limit <n>]
c1i attribute-mappings get <mapping-id>
c1i attribute-mappings create --app-id <app-id> --type <name-or-id> [--key <profile-key>] [--custom-key <attribute>] [--fallbacks-file <file>] [--profile-type-id <id>]... | --body-file <file>
c1i attribute-mappings update <mapping-id> [--key <profile-key>] [--custom-key <attribute>] [--fallbacks-file <file>]
c1i attribute-mappings delete <mapping-id>
```

`attribute-types list` rows are `id`, `display_name` and `config_kind`, which
decides what `create` needs: `app_user_profile` (Department, Job Title,
Employment Type, ...) needs `--key`, the profile key to read; `app` (Manager
Email, Directory Status) reads the app itself and takes no `--key`;
`custom_app_user_profile` (Custom) needs `--key` and `--custom-key`, the custom
user attribute that receives the value. `create` looks `--type` up by display
name or id (type ids are specific to a tenant), picks the request body from the
config kind, and refuses a wrong `--key` (exit `2`, where the server answers
`500`) or an unknown type (exit `4`) before sending anything; `--dry-run`
makes the same lookup. A `--type` display name shared by several types exits
`2` listing their ids. A `cel_expression` type is refused by flags; use
`--body-file`. `create` prints the mapping unwrapped: `jq -er .id`.
Rows of `attribute-mappings list` are `id`, `app_id`, `key` (null for an `app`
type), `display_name`, `attribute_type_id`, `attribute_type_name`,
`config_kind`, `priority`, `count` and `evaluation_error_count`; `--app-id`
filters after each page, so every page is still read.

```sh
c1i attribute-mappings create --app-id "$APP" --type "Employment Type" --key employment_type
c1i attribute-mappings create --app-id "$APP" --type "Manager Email"
```

Traps:

- Values are copied verbatim: a CSV `Full-Time` becomes `employmentType:
  "Full-Time"`, so a CEL comparison against a lowercase literal never matches.
- Evaluation is asynchronous and nothing signals completion. In the runs
  measured, users carried the value, and a changed `--key` showed on them,
  within a minute; re-read the user rather than trusting the command's exit
  code. The mapping's `count` stayed `0` throughout, so it is no readiness
  check.
- A second `create` for the same app and type is not refused: it makes a second
  mapping with its own id. List before creating in a script.
- An unknown `--app-id` exits `4` on a real run only: `--dry-run` looks up the
  type, not the app.
- The server sets `display_name` (the type's name, or `--custom-key`, as seen
  in one live run); there is no flag for it.
- `update` can change `--key`, `--custom-key` and `--fallbacks-file`, not the
  type or the app. The server's PUT replaces the whole mapping and ignores
  `updateMask`, so c1i reads the mapping first and sends back its current
  attribute type, fallbacks and profile types with only your flags changed
  (`--dry-run` reads too), so an edit made between the read and the write is
  overwritten. A `--fallbacks-file` holding `[]` (for example `echo '[]' | c1i
  attribute-mappings update <id> --fallbacks-file -`) clears the fallbacks.
- `--profile-type-id` binds profile types; the server refuses it on a built-in
  type (`builtin attributes cannot be bound to profile types`). Profile types
  have no create command here.
- Precedence when two apps map the same attribute type was not tested. Do not
  map a type that another app already maps on users that overlap.

`delete` removes the mapping: `get` then exits `4` and the lists omit it;
deleting again exits `0`. `--body-file` sends a full request verbatim, mutually
exclusive with the flags.

### Attribute values

Tenant attribute values are the values of the tenant's attribute types, such as
Compliance Framework (SOC2, HIPAA, ...) and Risk Level (Low, High, ...). This is
a different resource from `attribute-types`/`attribute-mappings` above, which
map an app's profile keys onto user fields.

```sh
c1i attributes types [--page-size <n>] [--page-token <token>] [--limit <n>]
c1i attributes list --type <name-or-id> [--page-size <n>] [--page-token <token>] [--limit <n>]
c1i attributes search [--type <name-or-id>]... [--id <value-id>]... [--exclude-id <value-id>]... [--value <v>] [--query <text>] [--limit <n>]
c1i attributes get <value-id>
c1i attributes create --type <name-or-id> --value <value>
c1i attributes delete <value-id>
```

Rows are `id`, `type_id`, `value`, `created_at`, `updated_at` and `deleted_at`
(`null` for a live value); `types` rows are `id` and `name`. `--type` takes a
type's name or id and is looked up first, also on `--dry-run`: an unknown type
exits `4` (the server answers it with an empty list on `list` and `search`, and
accepts it on `create`), and a name shared by several types exits `2` listing
their ids. `create` prints the new value unwrapped (`jq -er .id`); the server
accepted a repeated value, even one differing only in case, as a new value with
its own id, and rejects an empty one. `delete` is a soft delete: `list` and
`search` stop returning the value, `get` still does with `deleted_at` set, and
deleting it again exits `0`. There is no update. The API's per-type routes
(`/api/v1/attributes/compliance_frameworks`, `/api/v1/attributes/risk_levels`)
repeat these operations for one type and have no commands; use `c1i api`.

### Webhooks

Outbound webhooks are the registered https endpoints C1.ai calls, for example
from a policy's webhook approval or provision step (which name one by
`webhookId`).

```sh
c1i webhooks list [--page-size N] [--page-token TOKEN] [--limit N]
c1i webhooks search [--query TEXT] [--webhook-id ID]... [--page-size N] [--page-token TOKEN] [--limit N]
c1i webhooks get <webhook-id>
c1i webhooks create --display-name NAME --destination-url https://hooks.example.com/c1 [--description TEXT] [--callback-timeout 10m]
c1i webhooks update <webhook-id> [--display-name NAME] [--destination-url URL] [--description TEXT] [--callback-timeout DURATION]
c1i webhooks test <webhook-id>
c1i webhooks delete <webhook-id>
```

The endpoint flag is `--destination-url`, not `--url`, which stays the global
tenant flag. It must be an absolute `https://` URL with a host name or IPv4
address; anything else exits `2` before a request is sent. The server would
otherwise rewrite another scheme, or none, to `https` on create, and reject it
on update with `invalid url: only 'https' scheme is supported`. An IPv6 literal
is refused because the server strips its brackets, and `webhooks test` then
fails with `invalid port ":db8::1" after host`. A host the server will not call
is a `400`, `invalid url: restricted destination` (exit `2`).

A blank `--display-name` exits `2` on create and update, as described under
[Display names](#display-names); the server would store one on update.

`--callback-timeout` follows the [Durations](#durations) rule and must be from
`0` to `720h`; over `720h`, update returns `callback_timeout must be between 0
and 30 days`.

`update` sends only the flags you pass and masks exactly those, and with no
field flag it exits `2`; an empty `--description` is sent as an empty value. Updating a
soft-deleted webhook exits `0` and changes it, and it stays deleted. A webhook
has no signing secret, so create returns nothing that can't be read again with
`get`.

`webhooks list` pages are larger than `--page-size`: the server raises a size
below 5 to 5 and returns one row more than that, so use `--limit` for an exact
count.

`webhooks create` and `update` print the webhook, unwrapped (`jq -er .id`);
`webhooks delete` prints `{}`. `webhooks test` queues a delivery and returns
the queued instance with `"state": "WEBHOOK_STATE_PENDING"`, at exit `0`,
whether or not the URL is reachable. Its `.id` is the delivery and `.webhookId`
the webhook you tested. Exit `0` does not mean the endpoint answered.
`webhooks delete` is a soft delete (see [Soft delete](#soft-delete)); deleting
it again exits `0`.

`webhooks search` posts to `/api/v1/search/webhooks` and prints the same rows
as `list`. `--query` matched the display name; a word only in a description
matched nothing. `--webhook-id` (repeatable) restricts it to those webhooks;
both together must both match. The search index lags writes: a new webhook was
missing from an unfiltered search for a few seconds, and a deleted one may
still be found briefly, so use `list`/`get` for the current state.

### Hooks

Hooks are customer-configured interception points for tool calls: an event
(`pre-tool-use`, `post-tool-use` or `pre-output`), an optional CEL filter, a
priority, and one of a function, a built-in pattern or a JSON patch.

```sh
c1i hooks list [--page-size N] [--page-token TOKEN] [--limit N]
c1i hooks get <hook-id>
c1i hooks create --display-name NAME --event pre-tool-use --function-id FUNCTION_ID [--function-commit-id ID] [--description TEXT] [--filter-cel EXPR] [--priority N] [--enabled] [--managed-by-guardrails]
c1i hooks create --body-file hook.json
c1i hooks update <hook-id> [--display-name NAME] [--description TEXT] [--event EVENT] [--filter-cel EXPR | --clear-filter] [--priority N] [--enabled[=false]] [--managed-by-guardrails[=false]] [--function-id ID [--function-commit-id ID]]
c1i hooks update <hook-id> --body-file hook.json --update-mask builtinPattern
c1i hooks delete <hook-id>
```

The flags build a function hook. A built-in pattern hook (14 pattern configs)
and a JSON patch hook go through `--body-file`, a full Hook object; the server
refuses a JSON patch hook with `403` when its preview is not enabled for the
tenant. A hook is created disabled unless `--enabled` is passed; an enabled
hook intercepts tool calls. `list` rows carry `id`, `display_name`, `event`,
`kind` (`function`, `builtin_pattern`, `json_patch`), `enabled`,
`managed_by_guardrails` and `priority`; `get` shows the whole config. The
server did not paginate `list` when tried (`--page-size` had no effect).

`create` and `update` look up `--function-id` (and `--function-commit-id` under
it) first, `--dry-run` included, so an unknown one exits `4`; without the check
the server answers an unknown function with `400` and stores an unknown commit
id at exit `0`. `--body-file` is sent verbatim except that a `priority`
outside 0-1000, or one that is not a whole number, exits `2` when the mask
names it, since update does not validate it.

`update` sends only the flags you pass and names exactly those in the mask.
`--function-id` replaces the whole function reference (the commit goes back to
the published one unless `--function-commit-id` is given); `--clear-filter`
sends a null filter under the `filter` mask, which clears it. The server
leaves a field outside the mask alone and accepts an unknown mask path at exit
`0`. It does not validate `--filter-cel`: an uncompilable and a non-boolean
expression were both stored at exit `0`. Create refuses a hook with no type
field with `400` `hook function reference is missing function_id`.

`hooks delete` is soft: the hook leaves `list`, but `get` still returns it at
exit `0` with no marker (a Hook has no `deletedAt`), `update` still changes it,
and deleting it again exits `0`. The API says a hook referenced by a guardrail
rule cannot be deleted until the reference is removed (not observed). An
unknown id is `404` (exit `4`). `create`/`update` print the hook unwrapped
(`jq -er .id`); `delete` prints `{}`.

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
activity_name,actor.user.email_addr,time`). Both timestamps must sit within
about one year of now: the server rejects a future `--until` or a `--since`
older than ~365 days with a `400` (`value must be less than now within
8761h6m40s`).

#### System log exporters

```sh
c1i export exporters list
c1i export exporters search [--query TEXT] [--export-id ID ...]
c1i export exporters get <export-id>
c1i export exporters create --display-name NAME --datasource-id ID [--prefix P] [--format zstd|gzip]
c1i export exporters update <export-id> [--display-name NAME] [--prefix P] [--format zstd|gzip]
c1i export exporters delete <export-id>
c1i export exporters events <export-id>
```

An exporter continuously writes the tenant's audit events to a datasource; a
new one starts in `EXPORT_STATE_EXPORTING` and begins writing under `--prefix`
at once. No datasource API is exposed: take `--datasource-id` from an existing
exporter's `.datasource.datasourceId`. An unknown datasource id is a `400`,
`datasource not found`, which c1i reports as an unknown reference (exit `4`). `update` reads the exporter first and sends its
datasource back (the API refuses an update without one); the datasource id
cannot be changed. `delete` is soft (`get` still returns the exporter with
`deletedAt` set; `list` omits it); a repeat delete exits `0` and stamps
`deletedAt` again. `update` and `events` also succeed on a deleted exporter
(`update` changes it, exit `0`): check `deletedAt`. `search` has no `--name`: the
API's `displayName` filter returned an exporter whatever the name. `events`
reads the exporter first, so an unknown id exits `4` (the API answers it with an
empty `200`).

### Reports

```sh
c1i reports list
c1i reports get <report-id>
c1i reports delete <report-id>
c1i reports run <report-id>
c1i reports update <report-id> [--display-name N] [--description D] [--prompt P]
c1i reports program <report-id> --program-id ID
c1i reports provenance <run-id> --report-id ID
```

Saved reports of the reporting service (not `access-reviews reports`). There is
no create command: the API saves a report only from an already-rendered
reporting surface. `run` returns as soon as the run starts; poll with `get`.
A run's `artifactUrl` may be a signed URL (not observed): treat it as a secret.
These were checked live only on a tenant with no reports (unknown-id and
validation paths); the rest follows the API description.

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

# --limit caps the total and needs --paginate (exit 2 without)
c1i api --path /api/v1/apps --paginate --limit 20

# Force the array field to drain when auto-detection picks the wrong one
c1i api --path /api/v1/automation_executions --paginate --list-key automationExecutions
```

The method defaults to GET, or POST when a body is set; pass `--method` for
PUT/PATCH/DELETE. The body comes from `--body` (inline JSON) or `--body-file` (a
file, or `-` for stdin) — the two are mutually exclusive. GET and DELETE refuse
a body by default (a body on either is more likely a mistake than intent); pass
`--allow-delete-body` to lift that for DELETE specifically, for the handful of
C1 endpoints that require one. `--query key=value` and `--header key=value` are
both repeatable. A header name must be letters, digits and ``!#$%&'*+-.^_`|~``,
and a value may not hold a control character (newline, NUL, ...) except a tab.
The same holds for `mcp servers` `--header-name`/`--header-value`/`--bearer-token`
and for those fields, plus `awsSigv4` `accessKeyId` and `sessionToken`, inside
`--hosted-config-file`/`--external-config-file` in either JSON spelling
(`customHeader`/`custom_header`, `headerValue`/`header_value`, ...). A
violation is a usage error (exit 2) that names the flag or JSON path and never
echoes the text. A control character in a `--config-field` key or in a path flag
(`-o/--output`, `--out-dir`, `--*-file`) is also exit 2.

With `--paginate`, a `pageToken` in the body (or `page_token` in `--query`) is the first page's cursor and later pages replace it; `pageSize` is sent as given. Each page's first array-valued field is unwrapped and each
item is emitted as a single line of NDJSON, the same format list commands use.
This covers both the canonical `list` key and typed keys like
`automationExecutions`; use `--list-key <field>` to force a specific field
(naming one that no page so far has carried, on a page that has other array
fields, or one that is not an array, is a usage error, exit `2`). The
stuck-cursor guard described under Output Conventions applies, with items
compared by `id`, else by canonical JSON. A page holding only items already
seen is held, and printed once a later page brings a new item, the list ends,
or a later request fails, so only a stuck run is dropped; id-less items that
legitimately repeat across pages can trip the guard.

Without `--paginate`, the full JSON response is pretty-printed, and if it
carries a non-empty `nextPageToken`, `c1i` warns on stderr that the result is
partial and names `--paginate`, since a truncated page is otherwise
indistinguishable from a complete one at exit 0. stdout is unchanged either
way.

### API Discovery & Documentation

The `docs` commands require no C1 credentials — agents can use them to explore the API before authenticating.

```sh
# Print the agent bootstrap doc: output contracts, exit codes, when to
# prefer first-class commands over raw API calls (write to a file with --output)
c1i docs agents [--output AGENTS.md]

# Print the README and CHANGELOG embedded in the binary (no network). The
# README is the full reference; "docs agents" is the concise agent guide.
# Both take --output FILE ("-" is stdout, not a file named -).
c1i docs readme
c1i docs changelog

# Search documentation
c1i docs search "access reviews"

# Fetch a documentation page (a leading slash or the full docs URL also works)
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

# Print the command tree this binary accepts (no auth). --json adds full
# paths, aliases, counts, and the nested tree for verification scripts.
c1i docs map
c1i docs map --json
```

`docs map` is generated from the binary's own command tree, so it matches the
commands the running build accepts. Use it to check a README, guide, or public
docs page against the installed version; `--json` includes the version and
family/command/leaf counts.

`docs openapi`, `docs endpoints`, and `docs endpoint` use the current public API contract at `https://www.c1.ai/api/openapi.yaml`, including MCP server, tool, and toolset operations. The spec is cached for 24 hours at `~/.c1i/cache/api-openapi.yaml`; fetch failures fall back to that cache, however old, with a one-line warning on stderr naming its age. A response over 32 MiB counts as a failed fetch. With a cache to fall back on, a fetch makes one attempt unless you set `--max-retries` or `C1I_MAX_RETRIES`. With no cache, a failed fetch exits `4` for a `404`, `5` for a `429`, `6` for a `5xx`, and `8` for anything else, such as an unreachable host, a refused redirect or a redirect loop.

`docs search` and `docs page` send your query to a third-party documentation
search API (`api.mintlify.com`), not to the C1 API. The docs site is
https://c1.ai/docs/; `docs page` accepts a path after that prefix, a leading
slash, or the full URL.

`docs search` is a semantic search with no relevance threshold: every query returns up to 10 nearest matches, so even a nonsense query comes back with plausible-looking hits. A returned hit is not proof a concept exists, and an unexpected hit is not proof the thing you searched for is absent — read the snippet, or fetch the page with `docs page`, to judge. To check whether an API endpoint exists, use `docs endpoints --filter`, which has a real no-match.

`docs guide` is embedded static content (no network call), unlike `docs search` / `docs page`, which make a network call. Guides ship in two families: registering and operating MCP servers (`register-mcp-server`, `assign-toolset-everyone`, `test-mcp-gateway`, `decommission-mcp-server`) and everyday app/access-request workflows (`configure-new-app`, `request-access`, `inspect-and-approve-task`, `delegate-entitlement-provisioning`, `promote-idp-app-to-managed`). Run `c1i docs guide` with no argument for the full, current list.

`docs skill` is kept as an alias of `docs agents` for backward compatibility;
both print identical output.

`~/.c1i/cache/` belongs to c1i: when `docs openapi`, `docs endpoints` or `docs endpoint` refreshes the spec, any file there that the running version doesn't use (such as one left by an older release) is deleted. Don't keep your own files in it.

## Shell Completion

```sh
# bash
mkdir -p ~/.local/share/bash-completion/completions
c1i completion bash > ~/.local/share/bash-completion/completions/c1i

# zsh
c1i completion zsh > "${fpath[1]}/_c1i"

# fish
c1i completion fish > ~/.config/fish/completions/c1i.fish
```


```powershell
# PowerShell (load in the current session)
c1i completion powershell | Out-String | Invoke-Expression
```

Each generator takes `--no-descriptions` to emit a script that completes names
only, without the per-command help text.

## Version

```sh
c1i version       # or: c1i --version
```

## Updating the binary (`c1i upgrade`)

```sh
c1i upgrade                       # upgrade to the latest stable release (prompts first)
c1i upgrade --check               # print a JSON report; change nothing
c1i upgrade --channel latest -y   # take the newest release without prompting
```

`upgrade` follows a release channel from the C1.ai distribution center
(`dist.conductorone.com`): `stable` by default, or `latest`/`preview` via
`--channel`. It replaces a standalone binary only after verifying the release
manifest's Sigstore signature (signed by the release workflow run for that c1i
tag, recorded in Rekor) and the download's SHA-256 from that manifest.
`--yes`/`-y` skips the prompt and is required when stdin is not a terminal.
`--check` prints `current`, `latest`, `channel`, `update_available`,
`install_method` (`standalone`, `homebrew`, `go-install`, `container`, `system`,
or `windows`), and, for a Homebrew, `go install`, or container install,
`upgrade_command`. For those installs `upgrade` prints that command and exits 0
without replacing anything; Windows and system installs print a hint but no
`upgrade_command`. A source build (`dev` or a Go pseudo-version) is never
replaced: `upgrade` prints the channel's release and exits 0, and `--check`
reports `update_available: false`. `--dry-run` fetches and verifies the release (index,
manifest, signature, TUF root) and checks the install directory is writable,
then prints a JSON preview (see [Dry run](#dry-run)); it takes no lock, doesn't
prompt, and installs nothing. `--fields` is a usage error except with `--check`.

`upgrade` treats the distribution center as a system beyond C1: a dist `404`,
`429`, or `5xx` exits `4`, `5`, or `6`, but
any other dist `4xx`, a refused redirect, a redirect loop, a non-JSON `200`, or a TUF, signature,
or checksum failure exits `8`.

The channel list (`index.json`) and its yank flags are not signed, so a
compromised distribution origin could withhold upgrades or offer any signed
release newer than yours, yanked or prerelease, but never an unsigned build or
one older than the one you run.

## Uninstalling

Remove the binary the way you installed it: `brew uninstall c1i` for
Homebrew; for `go install`, delete `c1i` from `GOBIN` (set in the environment
or with `go env -w`), or from `$GOPATH/bin` (`~/go/bin` by default) when
`GOBIN` is unset; a downloaded binary is wherever you put it; and
`docker rmi public.ecr.aws/conductorone/c1i:<version>` for the container image.

Removing the binary leaves these behind:

- `~/.c1i/`, the OpenAPI spec cache.
- `~/.c1i.yaml`, the config file.
- `<UserConfigDir>/c1i/`, holding the file-fallback credentials
  (`credentials/`) and the access-token file cache (`tokens/`). See
  [Credential sources](#credential-sources) for each OS's config directory;
  on Linux it is `$XDG_CONFIG_HOME` when that is set.
- OS keyring entries: credentials under `c1i/<host>`, and cached access tokens
  (including tokens for `C1I_CLIENT_ID`/`C1I_CLIENT_SECRET` credentials) under
  `com.conductorone.c1i.tokens`. Older releases stored a
  `<name>.conductor.one` tenant's credentials under `c1i/<name>`.
- Any [shell completion](#shell-completion) script you installed, and the
  `c1i completion powershell` line if you added it to your PowerShell profile.

c1i can't list keyring entries or keep track of the tenants you've logged in to.
To remove its keyring credentials, run `auth logout` once per tenant before you
remove the binary. If `C1I_CLIENT_ID` and `C1I_CLIENT_SECRET` are both set, unset
them first: logout still removes the stored credentials, but drops the cached
token for the env-var credentials instead of theirs.

```sh
c1i auth logout --url example.conductor.one
```

Logout also drops the cached token for the credentials stored now. A token
cached under credentials you since replaced stays in the keyring. To remove
keyring entries by hand, replace `<host>` below with a tenant's host, such as
`example.conductor.one`, or with `<name>` for an entry an older release stored:

```sh
# macOS: each call deletes one item; repeat until it reports "could not be found"
security delete-generic-password -s 'c1i/<host>'
security delete-generic-password -s com.conductorone.c1i.tokens

# Linux (Secret Service): each call deletes every matching item
secret-tool clear service 'c1i/<host>'
secret-tool clear service com.conductorone.c1i.tokens
```

On Windows, open Credential Manager → Windows Credentials and remove
`c1i/<host>:client_id`, `c1i/<host>:client_secret`, and every
`com.conductorone.c1i.tokens:` entry. To find which hosts you have, search for
`c1i` in Keychain Access (macOS), Seahorse (Linux) or Credential Manager.

`~/.sigstore/root/`, which `upgrade` uses to verify releases, is shared with
other Sigstore tools, so leave it in place.

## License

Apache 2.0
