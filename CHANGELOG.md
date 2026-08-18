# Changelog

All notable changes to c1i are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project aims
to follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **`docs guide [name]`** — print an embedded, task-oriented runbook (no auth
  required, like the other `docs` subcommands). Run with no argument to list
  the available names. Ships with `register-mcp-server`, `assign-toolset-
  everyone`, and `test-mcp-gateway` (a pre-flight checklist for verifying a
  server's tools are approved, entitled, and served through the gateway).
  Content is embedded as Go string constants — no network call, unlike
  `docs search` / `docs page`.
- **`auth token`** — mint and print a short-lived OAuth2 bearer token from the
  stored credentials, for driving raw API calls yourself (e.g. `curl -H
  "Authorization: Bearer $(c1i auth token)"`). Prints just the token by default;
  `--json` also emits the token type and absolute expiry. The token is
  audience-scoped to the C1 API host and is never written to disk.
- **`mcp servers register --print-config-template --auth <mode>`** — emit a
  ready-to-edit `hostedConfig` / `externalConfig` JSON skeleton for the chosen
  auth method (`oauth2`, `aws-sigv4`, `google-service-account`, plus the simple
  methods), instead of hand-writing the file-based config. Valid JSON on stdout
  (guidance on stderr), so `--print-config-template --auth oauth2 2>/dev/null >
  config.json` yields a config that feeds straight back into `register
  --hosted-config-file`. `register --help` now also names the auth field shapes,
  documents the `tokenSharing` × auth-method compatibility rules, and links to
  the api-reference page.
- **`apps create`** — create a new app (a container to register MCP servers
  under) via `POST /api/v1/apps`. Only `--display-name` is required;
  `--description` is optional. Honors `--dry-run`. Previously the zero-state
  flow dropped to the raw `api` escape hatch.
- **`apps delete <app-id>`** — soft-delete an app via `DELETE /api/v1/apps/{id}`
  (sets `deletedAt`, retained for audit). Complements `apps create` so a
  container app made by mistake can be cleaned up without the raw `api` escape
  hatch. Honors `--dry-run`.
- **`apps set-owners <app-id> --user-id …`** — set an app's owner list via
  `PUT /api/v1/apps/{id}/owners` (replaces the full set; `--user-id` repeatable).
  Owner provisioning is asynchronous, so the command notes that new owners take
  ~60-90s to appear in `apps get`; a success means the request was accepted.
  Honors `--dry-run`.
- **`apps set-owners --wait` / `--wait-timeout`** — optionally block after the
  `PUT` and poll `GET /api/v1/apps/{id}/ownerids` (every 12s, default timeout
  4m) until every requested `--user-id` shows up as provisioned, printing
  progress as it goes. On timeout, exits non-zero with a message clarifying
  that provisioning is still pending and not necessarily a failure — owner
  provisioning has been observed to take from under two minutes up to several.
  Without `--wait`, behavior is unchanged; `--wait` combined with `--dry-run`
  still only previews the `PUT` and never polls.
- **`mcp gateway list-tools` / `mcp gateway call`** — drive the C1 MCP gateway
  over its streamable-HTTP MCP transport (the same handshake an MCP host does:
  initialize → notifications/initialized → tools/list / tools/call), closing the
  configure-then-verify loop so you can list and invoke a registered server's
  tools without hand-rolling the protocol. The gateway URL is derived from
  `--url` (inserting `-mcp` into the host) or set with `--gateway-url`; your
  standard C1 token is accepted, so no extra auth setup is needed. `list-tools`
  emits NDJSON (`--full` adds each tool's input schema); `call <tool> --args
  '{…}'` prints the tool result.

### Changed

- **BREAKING — ID arguments are now positional.** A command that addresses one
  existing resource by its own id now takes that id as the **first positional
  argument** instead of a flag; parent/scope ids remain flags. This makes the
  whole CLI consistent (flat commands like `users get <user-id>` already worked
  this way). Migrate:
  - `mcp servers get\|update\|update-credentials\|delete\|resync-tools --connector-id X --app-id A`
    → `mcp servers <verb> X --app-id A` (and `test-connection` takes `[<connector-id>]` positionally).
  - `mcp tools get\|approve\|delete\|history --id X --app-id A --connector-id C`
    → `mcp tools <verb> X --app-id A --connector-id C`.
  - `mcp toolsets get\|update\|delete --id X …` → `mcp toolsets <verb> X …`;
    `mcp toolsets get-by-entitlement --app-entitlement-id X --app-id A` → `… X --app-id A`;
    `mcp toolsets requestable-connectors --user-id X` → `… X`.
  - `tasks approve\|deny\|comment --task-id X` → `tasks <verb> X`.
  - `accounts set-owner --app-user-id X --app-id A --user-id U` → `accounts set-owner X --app-id A --user-id U`.
  Collection (`list`/`search`), create, and relationship (`mcp bindings *`)
  commands are unchanged (their ids stay flags). The old flags now error with
  "unknown flag". The convention is documented in CLAUDE.md and enforced across
  the README and the embedded agent skill.
- **Ctrl-C now cancels cleanly.** The root command wires `cmd.Context()` to a
  `signal.NotifyContext` on SIGINT/SIGTERM, so a long-running command (e.g.
  `apps set-owners --wait` polling for async owner provisioning) sees its
  context canceled and can exit with a clear message instead of the process
  being hard-killed with no output. A first Ctrl-C cancels gracefully; a
  second reverts to the OS default hard-kill.
- **`mcp servers catalog list`** rows now include `base_url`, `default_tool_prefix`,
  `stable`, `required_scope_count`, and `optional_scope_count` (in addition to
  the existing fields, kept as-is). The catalog holds many near-duplicate
  entries for the same service — a thin REST wrapper (`slack`, base_url
  `https://slack.com/api`) alongside the vendor's own hosted MCP endpoint
  (`slack-mcp`, base_url `https://mcp.slack.com/mcp`) — and `display_name` /
  `service_name` alone didn't reliably tell them apart. `required_scope_count`
  / `optional_scope_count` summarize each entry's OAuth scope tiering, which
  turns out to live per auth mode (`authModes[].scopes` vs `.optionalScopes`)
  rather than as a single catalog-wide list; the entry-level `defaultScopes`
  field some assumed carried it is empty on every catalog entry seen in
  production. `mcp servers catalog get --help` documents the details.
- An **empty required flag value** (e.g. `--app-id ""`) now exits `2` (usage),
  matching a missing required flag, instead of `1` (generic). The check
  (`requireNonEmpty`) applies this consistently across every command that uses
  it, so automation branching on exit codes sees a stable usage signal.
- **BREAKING — `mcp gateway call` now exits `7` when the tool itself fails.**
  Previously, a tool result with `isError: true` (the tool ran, but reported
  its own failure — e.g. a timed-out deployment) exited `0` like a success,
  because nothing inspected `isError`; only a transport/protocol failure (a
  non-2xx HTTP status, or a JSON-RPC `error` response) was ever non-zero. The
  new exit code `7` is distinct from the existing 3/4/5/6 transport codes —
  it means "the call completed but the tool reported an error," a different
  failure class entirely. **The full result is still printed to stdout
  exactly as before**, `isError` and all, so an in-band consumer (e.g. an
  LLM host reading the error text out of the `content` array) is unaffected;
  only the process exit code changes. `mcp gateway call` shipped earlier
  today (this same day) in the PR this follows up on, so there is
  effectively no installed base depending on the old exit-0 behavior.

### Fixed

- **`--fields` now bridges snake_case/camelCase.** List commands emit rows in
  snake_case while single-object reads emit camelCase, so `--fields displayName`
  on a list command silently returned `{}`. Field matching now falls back to a
  case- and separator-insensitive comparison when an exact key match misses, so
  a projection in either style resolves against either output. Exact matches are
  unchanged (they always win), and the output keeps the source key spelling —
  `--fields` selects keys, it never renames them.
- **`mcp gateway` failures now classify to the standard exit-code taxonomy.**
  A gateway HTTP failure previously exited `1` for everything except a 401/403
  at the handshake step (which alone was mapped to `3`); a 404/429/5xx from
  `list-tools` or `call` — including any failure after a successful handshake,
  since the one-off classifier only ran on `Initialize` — was indistinguishable
  from a generic error. `*mcpgateway.HTTPError` now unwraps to a
  `*client.APIError`, so every gateway call threads through the same taxonomy
  every other API failure gets (401/403 → 3, 404 → 4, 429 → 5, 5xx → 6),
  without losing the response body from the error message. The one-off
  handshake-only classifier is removed as redundant.
- **`extractSSEResponse` now follows the SSE spec exactly.** Multiple `data:`
  lines within one SSE event are joined with `\n` (previously concatenated
  with no separator, which could corrupt a multi-line payload), and exactly
  one optional leading space after `data:` is stripped — previously
  `TrimSpace` also ate meaningful leading/trailing whitespace inside the
  payload. The response event is now selected by matching the request's
  JSON-RPC `id` first, falling back in order to an event carrying `result`/
  `error`, then the last event, then the raw body on a scan error — so a
  reply to a different in-flight request can no longer be mistaken for the
  caller's own.

## [0.3.0] - 2026-07-16

### Added

- **`requests list`** — the requester lens on access requests (the grant and
  revoke tasks you file), backed by `POST /api/v1/search/tasks` scoped to those
  task types. By default it lists requests you opened or are the subject of, so
  after a `requests create` you can poll status without dropping to `api`. Scope
  with `--user-id` (another user) or `--all` (whole tenant), and narrow with
  `--app-id`, `--entitlement-id`, `--state open|closed`, and `--type
  grant|revoke`. Complements `tasks list`, which is the approver's My Work lens.
- **`requests get <request-id>`** — fetch a single access request (the `task_id`
  returned by `requests create`) via `GET /api/v1/tasks/{id}`, returned as pretty
  JSON including its current policy step and outcome.
- **`export events`** — bulk-export the C1 system log (OCSF-formatted audit
  events) as an NDJSON stream, one event per line, auto-paginating the full
  result set via `POST /api/v1/systemlog/events`. Redirect to a file to archive
  events or ship them to an external system. Filter with `--since` / `--until`
  (RFC3339), order with `--sort asc|desc` (default `asc`, chronological), and
  resume an incremental sync with `--since-event-uid`. `--fields` projection
  applies per event.
- **`mcp servers`** — manage the MCP-server lifecycle over REST (newly exposed by
  the C1 API). Reads: `list`, `get`, `search` (with per-server tool counts),
  `catalog list`/`get` (browse HOSTED templates), and `connections list` (the
  caller's per-user connections). Lifecycle: `register` (HOSTED via `--catalog-id`
  or EXTERNAL via `--url`), `update` (metadata via update_mask), `delete`, and
  `resync-tools`. Config helpers: `update-credentials`, `test-connection`, and
  `discover-oidc`. Auth uses convenience flags for the simple methods (`--auth
  none|bearer-token|custom-header|basic-auth`) plus a `--hosted-config-file` /
  `--external-config-file` JSON escape hatch for OAuth2 / AWS SigV4 / Google
  service-account configs; secrets are sealed server-side and never returned on
  read. Mutations honor `--dry-run`.

## [0.2.1] - 2026-07-10

### Added

- **`grants list`** — query access grants (who has access to what), backed by
  `POST /api/v1/search/grants`. Filter by `--app-id`, `--user-id`,
  `--app-user-id`, or `--entitlement-id` (with `--app-id`); at least one filter
  is required. Each NDJSON row carries the entitlement, the account and its
  identity user, grant timestamps, and `grant_source_count` (0 = direct grant,
  otherwise the number of groups/roles the access is inherited through).
- **`get <id>` for core resources**: `c1i users get`, `c1i apps get`, and
  `c1i entitlements get <id> --app-id` return a single object as pretty JSON,
  removing the need to `list | grep`.
- **`--dry-run` / `C1I_DRY_RUN`**: preview a mutating request (method, path,
  pretty-printed body) without sending it. Covers every write command
  (`requests create`, `tasks approve`/`deny`/`comment`, `accounts set-owner`, the
  `mcp` mutations) and non-GET `api` calls. Previews run without credentials,
  except `tasks approve`/`deny`, which authenticate to resolve the task's current
  policy step.
- **`--debug` / `C1I_DEBUG`**: trace each HTTP request to stderr (method, URL,
  status, elapsed time, including retries). Headers and bodies are never logged.
- **`api` escape hatch rounded out**: `--method PATCH` is now supported;
  `--body-file` reads the JSON body from a file (or `-` for stdin, mutually
  exclusive with `--body`); `--query key=value` and `--header key=value` are both
  repeatable.

### Fixed

- README documented `api --method` as GET/POST/PUT/DELETE only; PATCH is now
  supported and the docs list all five methods.

## [0.2.0] - 2026-07-10

First changelog entry; releases through v0.1.5 predate this file (see the
[GitHub releases](https://github.com/ConductorOne/c1i/releases)).

### Added

- **Automatic retries** for transient API failures, with exponential backoff +
  jitter honoring `Retry-After`. `429` is retried for any method; `5xx`
  (500/502/503/504) and network errors are retried only for idempotent methods
  (GET/PUT/DELETE), never for POST. Configure with `--max-retries` /
  `C1I_MAX_RETRIES` (default `4`; `0` disables).
- **Output field projection** via `--fields` / `C1I_FIELDS` — trim emitted JSON
  to selected dot-path keys (e.g. `id,user.email`), preserving nesting. Applies
  to list, single-object `get`, and `api` output; a big token saver for agents.
- **Structured errors and exit codes**: `--error-format json` /
  `C1I_ERROR_FORMAT` emits `{error,status,method,path,body}`, and the process
  exits with a code callers can branch on — `0` ok, `1` generic, `2` usage,
  `3` auth (401/403), `4` not-found (404), `5` rate-limited (429), `6` server
  (5xx).
- `tasks approve` / `tasks deny` accept `--policy-step-id` (auto-derived from the
  task's current step when omitted).

### Changed

- Auth output now names the credential backend in use (e.g. "macOS Keychain",
  or the file path) on `login` and `status`; `logout` reports whether anything
  was removed and warns when env-var credentials still override.
- Documentation refreshed across the README, the embedded agent skill
  (`c1i docs skill`), and the command reference to cover the above.

### Fixed

- Headless Linux: fall back to the 0600 file credential store when the OS
  keyring is unavailable (e.g. no `dbus-launch`), instead of failing login.
- Access-request bodies (`task/grant`, `task/revoke`) are sent flat, not wrapped
  under a `task` key.
- `tasks approve`/`deny`/`comment` parse the updated task from `taskView.task`
  (previously printed empty `task_id=`/`state=`).
- IDs interpolated into request paths are URL-escaped (`client.Path`), so values
  containing `/`, `?`, `#`, or spaces address the intended resource.
- `Retry-After` parsed as 64-bit (32-bit safe); numeric precision preserved in
  projected and paginated output (no float64 rounding of large integers).
- `--error-format` now rejects unrecognized values instead of silently
  degrading to text.

### Internal

- CI enforces `gofmt` via golangci-lint; module-wide formatting normalized.

[0.3.0]: https://github.com/ConductorOne/c1i/releases/tag/v0.3.0
[0.2.1]: https://github.com/ConductorOne/c1i/releases/tag/v0.2.1
[0.2.0]: https://github.com/ConductorOne/c1i/releases/tag/v0.2.0
