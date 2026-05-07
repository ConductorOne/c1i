# c1i

**C1 Interface** — an agent-oriented CLI for C1, and it looks like `cli`. Get it?

> **Alpha** — under active development. Commands, flags, and output formats may change without notice.

A command-line interface for the [C1](https://www.conductorone.com) API designed for AI agents.
Structured output (NDJSON/JSON), built-in API docs, and auto-pagination.
For a human-friendly CLI, see [cone](https://github.com/ConductorOne/cone).

## Quick Start

```sh
# Install
go install github.com/ConductorOne/c1i@latest

# Log in (opens browser)
c1i auth login --url mycompany.conductor.one

# List users
c1i users list

# Explore the API — no credentials needed
c1i docs search "access reviews"
c1i docs endpoints --filter task
```

## Commands

### Users

```sh
c1i users list [--query <text>] [--email <email>] [--status enabled|disabled|deleted] [--page-size N] [--page-token TOKEN]
```

### Apps

```sh
c1i apps list [--page-size N] [--page-token TOKEN]
```

### Accounts

```sh
c1i accounts list --app-id <id> [--status enabled|disabled|deleted] [--type user|service_account|system_account] [--unmapped-only] [--query <text>] [--page-size N] [--page-token TOKEN]

c1i accounts set-owner --app-id <id> --app-user-id <id> --user-id <id>
```

### Entitlements

```sh
c1i entitlements list [--app-id <id>] [--query <text>] [--page-size N] [--page-token TOKEN]
```

### Tasks

```sh
c1i tasks list [--state open|closed] [--query <text>] [--assigned-to-me] [--page-size N] [--page-token TOKEN]
c1i tasks approve --task-id <id> [--comment <text>]
c1i tasks deny --task-id <id> [--comment <text>]
c1i tasks comment --task-id <id> --comment <text>
```

### Connectors

```sh
c1i connectors list --app-id <id> [--page-size N] [--page-token TOKEN]
```

### Access Requests

```sh
c1i requests create grant --app-id <id> --entitlement-id <eid> --user-id <uid> [--justification <text>] [--duration <duration>]
c1i requests create revoke --app-id <id> --entitlement-id <eid> --user-id <uid> [--justification <text>]
```

### Raw API

```sh
# GET request
c1i api --path /api/v1/apps

# POST request
c1i api --path /api/v1/search/users --body '{"pageSize":10}'

# Auto-paginate through all results (NDJSON output, one item per line)
c1i api --path /api/v1/apps --paginate
```

When `--paginate` is used, the `list` array from each page is unwrapped and each item is emitted as a single line of NDJSON — the same format used by list commands. Without `--paginate`, the full JSON response is pretty-printed.

### API Discovery & Documentation

The `docs` commands require no C1 credentials — agents can use them to explore the API before authenticating.

```sh
# Search documentation
c1i docs search "access reviews"

# Fetch a documentation page
c1i docs page product/admin/campaigns

# List API endpoints (filtered)
c1i docs endpoints --filter task

# Show full request/response schema for an endpoint
c1i docs endpoint /api/v1/search/tasks

# Dump the raw OpenAPI spec
c1i docs openapi
```

## Output Conventions

- **List commands** (`users list`, `apps list`, etc.) output NDJSON (one JSON object per line).
- **`api`** outputs pretty-printed JSON. With `--paginate`, outputs NDJSON (one list item per line).
- **`docs`** commands output NDJSON (`search`, `endpoints`), pretty JSON (`endpoint`, `openapi` is YAML), or plain text (`page`).
- List commands auto-paginate by default. Pass `--page-token` to fetch a single page manually.

## Configuration

c1i requires a C1 **URL**. You can pass a full URL, a raw domain, or a legacy short tenant name. Set it via (in order of precedence):

1. `--url` flag
2. `C1I_URL` environment variable
3. `~/.c1i.yaml` config file:
   ```yaml
   url: https://mycompany.conductor.one
   ```

All of these are equivalent:
- `--url https://mycompany.conductor.one`
- `--url mycompany.conductor.one`
- `--url mycompany`

For credential storage, see [Credential sources](#credential-sources) below.

## Authentication

```sh
# Browser-based login (OAuth device flow)
c1i auth login

# Or store credentials directly
c1i auth login --client-id <id> --client-secret <secret>

# Check credential status (also reports the storage backend)
c1i auth status

# Show the authenticated principal: user ID, display name, email, role/permission/feature counts
c1i auth whoami           # add --verbose for full roles/permissions/features arrays

# Remove stored credentials
c1i auth logout
```

### Credential sources

`c1i` reads credentials from the first source that has them, in this order:

1. **Environment variables** — set `C1I_CLIENT_ID` and `C1I_CLIENT_SECRET`
   (alongside `C1I_URL`) for non-interactive / CI use. Both must be set; if
   only one is set the value is ignored.
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
the active credentials.

## Shell Completion

```sh
# bash
c1i completion bash > /etc/bash_completion.d/c1i

# zsh
c1i completion zsh > "${fpath[1]}/_c1i"

# fish
c1i completion fish > ~/.config/fish/completions/c1i.fish
```

## Version

```sh
c1i version       # or: c1i --version
```

## Design

c1i is built specifically as a tool for AI agents:

- **Structured output**: All data commands produce NDJSON or JSON — never mixed or human-formatted output.
- **Self-documenting API**: `docs endpoints`, `docs endpoint`, and `docs search` let an agent discover and understand the C1 API without external documentation.
- **Predictable pagination**: List commands auto-paginate; `--page-token` gives manual control when needed.
- **Raw API escape hatch**: `api --path` with `--paginate` lets an agent hit any endpoint, even ones without a native command. Paginated output uses the same NDJSON format as list commands.

## License

Apache 2.0
