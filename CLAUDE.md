# c1i

CLI for the C1 (formerly ConductorOne) API. Go module: `github.com/ConductorOne/c1i`.

## Branding

The product has been rebranded from **ConductorOne** to **C1**. Rules:

- All user-facing text (help strings, CLI output, README) should use **"C1"**, not "ConductorOne".
- The GitHub org remains `ConductorOne` — do not rename import paths or the module.
- The legal entity is still "ConductorOne, Inc." — do not change LICENSE or copyright notices.
- Domain names (`conductorone.com`, `conductor.one`) are unchanged.

## Build & Test

```sh
go build ./...
go test ./...
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
