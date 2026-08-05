# Changelog

All notable changes to c1i are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project aims
to follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

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

### Changed

- An **empty required flag value** (e.g. `--app-id ""`) now exits `2` (usage),
  matching a missing required flag, instead of `1` (generic). The check
  (`requireNonEmpty`) applies this consistently across every command that uses
  it, so automation branching on exit codes sees a stable usage signal.

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
