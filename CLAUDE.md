# c1i

CLI for the C1 (formerly ConductorOne) API. Go module: `github.com/ConductorOne/c1i`.

## Branding

The product has been rebranded from **ConductorOne** to **C1**. Rules:

- All user-facing text (help strings, CLI output, README) should use **"C1"**, not "ConductorOne".
- The GitHub org remains `ConductorOne` — do not rename import paths or the module.
- The legal entity is still "ConductorOne, Inc." — do not change LICENSE or copyright notices.
- Domain names (`conductorone.com`, `conductor.one`) are unchanged.

## Build & Test

Run the full check before pushing — CI enforces gofmt via golangci-lint, so
`go build`/`go test` alone is not enough:

```sh
go build ./...
go vet ./...
go test ./...
golangci-lint run --timeout=3m ./...   # includes gofmt formatting; CI gates on this
```

## Project Layout

- `cmd/` — Cobra command definitions (one file per command)
- `internal/client/` — Authenticated HTTP client
- `internal/config/` — URL parsing and keychain service helpers
- `internal/keychain/` — Credential storage. Three backends, in precedence order: `C1I_CLIENT_ID`/`C1I_CLIENT_SECRET` env vars (read-only), OS keyring (go-keyring), and a 0600 file under `os.UserConfigDir()` (fallback for headless Linux/CI/containers).
- `internal/login/` — OAuth device flow
- `internal/tokensource/` — OAuth2 token source

## Conventions

- Output: NDJSON for list/search commands, pretty JSON for single-object commands, plain text for auth.
- All list commands auto-paginate. `--page-token` disables auto-pagination.
- `docs` subcommands require no authentication.

### Global flags (persistent, on `rootCmd`)

- `--url` / `C1I_URL`, `--fields` / `C1I_FIELDS` (JSON field projection),
  `--max-retries` / `C1I_MAX_RETRIES` (default `client.DefaultMaxRetries`),
  `--error-format` / `C1I_ERROR_FORMAT` (`text`|`json`). See README for behavior.

### Patterns to follow when adding/changing commands

- **API client:** build it with `newClient(cmd, baseURL)` (cmd/client.go), not
  `client.New` directly — the helper threads the global flags (retries, etc.).
- **Paths:** interpolate IDs into request paths with `client.Path("…/%s", id)`,
  never `fmt.Sprintf` — `Path` URL-escapes each segment (a raw ID with `/`, `?`,
  `#`, or a space would otherwise mis-address the resource).
- **Output:** list rows go through the `newEmitter(...)`/`.Encode` emitter;
  single-object reads through `writeObject`; **mutation confirmations**
  (create/update/delete) through `writeRawObject` (never projected, so a
  session-wide `C1I_FIELDS` can't blank a success message).
- **Errors:** the client returns typed `client.APIError` (carries status) and
  `client.AuthError`; `cmd/errors.go` maps them to exit codes — 0 ok, 1 generic,
  2 usage, 3 auth (401/403), 4 not-found (404), 5 rate-limited (429), 6 server
  (5xx). Wrap client errors with `%w` so `errors.As` can classify them.
- Retries (429/5xx + transport, idempotent-aware) live in the client
  (`internal/client/client.go`); commands get them for free via `newClient`.
