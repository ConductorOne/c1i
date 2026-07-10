# Changelog

All notable changes to c1i are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project aims
to follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

[0.2.0]: https://github.com/ConductorOne/c1i/releases/tag/v0.2.0
