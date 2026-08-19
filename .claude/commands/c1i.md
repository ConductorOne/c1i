Run: `go run .` (from the repo root)

The full, current command-by-command reference (every subcommand, its flags,
positional-arg conventions, and NDJSON field shapes) lives in `cmd/skill.md` —
run `go run . docs skill` to print it, or open the file directly. That file is
the single source of truth for command syntax; this doc used to carry a second
copy of it and the two drifted (stale flags, missing subcommands, a missing
exit code) until this pass. Don't re-add a per-command copy here — extend
`cmd/skill.md` instead, and if a flag/behavior claim ever looks off, verify
against `go run . <cmd> --help` rather than either doc.

## Auth

```sh
go run . auth login                                          # OAuth device flow
go run . auth login --client-id=ID --client-secret=SECRET    # direct credential login
go run . auth status                                          # verify stored credentials + backend in use
go run . auth whoami                                          # show the authenticated principal
go run . auth logout                                          # remove stored credentials
```

Credentials resolve in order: `C1I_CLIENT_ID`/`C1I_CLIENT_SECRET` env vars
(read-only) → OS keyring → a `0600` JSON file under `os.UserConfigDir()`
(headless Linux/CI fallback).

## Configuration

A C1 URL is required for all API commands, resolved in order: `--url` flag →
`C1I_URL` env var → `~/.c1i.yaml` (`url: https://mycompany.conductor.one`).
`--url=mycompany` and `--url=mycompany.conductor.one` are both accepted.

## Global flags (persistent, on every command)

- `--fields=a,b,c` / `C1I_FIELDS` — project emitted JSON to these dot-paths;
  missing keys are silently omitted. Never trims mutation/auth confirmations.
- `--max-retries=N` / `C1I_MAX_RETRIES` (default 4, `0` disables) — retries
  429 (any method) and 5xx/network errors (idempotent GET/PUT/DELETE only)
  with backoff, honoring `Retry-After`.
- `--error-format=text|json` / `C1I_ERROR_FORMAT` — `json` emits a structured
  error object instead of `Error: ...` text.
- `--dry-run` / `C1I_DRY_RUN` — preview a mutating request's method/path/body
  without sending it; exits 0.
- `--debug` / `C1I_DEBUG` — trace HTTP method/URL/status/timing to stderr
  (never headers or bodies).

## Errors & exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | generic / unclassified error |
| 2 | usage error (bad flags/args, unknown command) |
| 3 | not authenticated, or API `401`/`403` |
| 4 | API `404` (not found) |
| 5 | API `429` (rate limited) |
| 6 | a remote system failed: API `5xx`, or an upstream MCP connector failed |
| 7 | MCP tool call completed but the tool itself reported `isError: true` |

Typed internally via `client.APIError`/`client.AuthError`, classified in
`cmd/errors.go`.

## Documentation

```sh
go run . docs search "access reviews"       # search docs (no auth required)
go run . docs page product/admin/campaigns  # fetch a doc page
go run . docs endpoints --filter=task       # list API endpoints (filterable)
go run . docs endpoint /api/v1/search/tasks # full request/response schema
go run . docs openapi                       # dump raw OpenAPI spec
go run . docs skill                         # print the embedded agent skill doc (cmd/skill.md)
```
