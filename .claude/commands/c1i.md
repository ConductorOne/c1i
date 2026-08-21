Run: `go run .` (from the repo root)

The agent-facing bootstrap doc (tenant/auth, output contracts, exit codes,
pagination, and when to prefer a first-class command over raw `api`) lives in
`cmd/agents.md` — run `go run . docs agents` to print it, or open the file
directly. (`go run . docs skill` is kept as an alias for backward
compatibility; it prints identical output.) It deliberately does not
enumerate every subcommand/flag — the cobra command tree itself
(`go run . --help`, `go run . <group> --help`) is the source of truth for
that and can't drift the way a static doc can. This doc used to carry a
second copy of the per-command reference and the two drifted (stale flags,
missing subcommands, a missing exit code) until that was cut. Don't re-add a
per-command copy here; if a flag/behavior claim ever looks off, verify
against `go run . <cmd> --help` rather than any doc.

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
`--url` takes a full host — `mycompany.conductor.one` or `mycompany.c1eu.ai`,
with or without the scheme; `https` is required, and any other scheme is
rejected rather than rewritten. A bare `mycompany` is rejected as ambiguous.

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
| 2 | usage error (bad flags/args, unknown command, an empty id argument, an id the API redirects to a collection, or any API `4xx` other than `401`/`403`/`404`/`408`/`429`) |
| 3 | not authenticated, or API `401`/`403` |
| 4 | API `404` (not found) |
| 5 | API `429` (rate limited) |
| 6 | C1 failed: API `5xx` or `408`, a `200` with a body that isn't JSON, or a redirect loop (`RedirectLoopError`) |
| 7 | MCP tool call completed but the tool itself reported `isError: true` |
| 8 | a system beyond C1, or the MCP protocol layer, failed (includes an unreachable gateway) |

Typed internally and classified in one place, `cmd/errors.go`: from the client,
`APIError`, `AuthError`, `PathError`, `RedirectError`, `RedirectLoopError`; from
the gateway, `mcpgateway.TransportError` (unreachable); plus package-`cmd`
wrappers `usageError`, `toolExecutionError`, `nonJSONResponseError`, and
`upstreamError`.

## Documentation

```sh
go run . docs search "access reviews"       # search docs (no auth required)
go run . docs page product/admin/campaigns  # fetch a doc page
go run . docs endpoints --filter=task       # list API endpoints (filterable)
go run . docs endpoint /api/v1/search/tasks # full request/response schema
go run . docs openapi                       # dump raw OpenAPI spec
go run . docs agents                        # print the embedded agent bootstrap doc (cmd/agents.md)
go run . docs guide                         # list embedded task-oriented runbooks
```
