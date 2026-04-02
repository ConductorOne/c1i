# c1i

A command-line interface for the [ConductorOne](https://www.conductorone.com) API.

## Install

```
go install github.com/ductone/c1i@latest
```

## Configuration

c1i requires a **tenant** name. Set it via (in order of precedence):

1. `--tenant` flag
2. `C1I_TENANT` environment variable
3. `~/.c1i.yaml` config file:
   ```yaml
   tenant: mycompany
   ```

Credentials are stored in the macOS keychain per tenant (service: `c1i/<tenant>`).

## Authentication

```sh
# Store and verify API credentials
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

# Auto-paginate through all results
c1i api --path /api/v1/apps --paginate
```

### Documentation

Browse the ConductorOne API reference and docs without leaving the terminal.

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

- **List commands** (`users list`, `apps list`, etc.) output NDJSON (one JSON object per line), suitable for piping to `jq`.
- **Auth commands** output human-readable text.
- **`api`** outputs pretty-printed JSON.
- List commands auto-paginate by default. Pass `--page-token` to fetch a single page manually.

## License

Apache 2.0
