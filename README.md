# c1i

> **Alpha** — this tool is under active development. Commands, flags, and output formats may change without notice.

A command-line interface for the [ConductorOne](https://www.conductorone.com) API, designed for both human and AI agent use.

c1i outputs structured data (NDJSON, JSON) that's easy for agents to parse, and includes built-in API discovery via `docs` commands — so an agent can explore the ConductorOne API, look up endpoint schemas, and search documentation without any additional credentials or tools.

## Install

```
go install github.com/ductone/c1i@latest
```

## Configuration

c1i requires a ConductorOne **URL**. You can pass a full URL, a raw domain, or a legacy short tenant name. Set it via (in order of precedence):

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

Credentials are stored in the macOS Keychain, keyed per host.

## Authentication

```sh
# Browser-based login (OAuth device flow)
c1i auth login

# Or store credentials directly
c1i auth login --client-id <id> --client-secret <secret>

# Check credential status
c1i auth status
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
c1i tasks list [--state open|closed] [--query <text>] [--page-size N] [--page-token TOKEN]
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

Browse the ConductorOne API reference and docs without leaving the terminal. The `docs` commands require no ConductorOne credentials — agents can use them to explore the API before authenticating.

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

- **List commands** (`users list`, `apps list`, etc.) output NDJSON (one JSON object per line), suitable for piping to `jq` or parsing line-by-line.
- **Auth commands** output human-readable text.
- **`api`** outputs pretty-printed JSON. With `--paginate`, outputs NDJSON (one list item per line).
- **`docs`** commands output NDJSON (search), plain text (page), or JSON (endpoints, endpoint).
- List commands auto-paginate by default. Pass `--page-token` to fetch a single page manually.

## Agent Integration

c1i is designed to work well as a tool for AI coding agents:

- **Structured output**: All data commands produce NDJSON or JSON, never mixed human-readable formats.
- **Self-documenting API**: `docs endpoints`, `docs endpoint`, and `docs search` let an agent discover and understand the ConductorOne API without external documentation.
- **Predictable pagination**: List commands auto-paginate; `--page-token` gives manual control when needed.
- **Raw API escape hatch**: `api --path` with `--paginate` lets an agent hit any endpoint, even ones without a native command. Paginated output uses the same NDJSON format as list commands.

## License

Apache 2.0
