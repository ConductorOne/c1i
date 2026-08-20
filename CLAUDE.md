# c1i

CLI for the C1 (formerly ConductorOne) API. Go module: `github.com/ConductorOne/c1i`.

## Branding

The product has been rebranded from **ConductorOne** to **C1**. Rules:

- All user-facing text (help strings, CLI output, README) should use
  **"C1"**, not "ConductorOne".
- The GitHub org remains `ConductorOne` — do not rename import paths or the
  module.
- The legal entity is still "ConductorOne, Inc." — do not change LICENSE or
  copyright notices.
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

Never weaken, loosen, or delete a test to make a change pass. For a new test,
confirm it fails before your fix and passes after — a test that compiles but
never fails proves nothing.

## Project Layout

- `cmd/` — Cobra command definitions (one file per command)
- `internal/client/` — Authenticated HTTP client
- `internal/config/` — URL parsing and keychain service helpers
- `internal/keychain/` — Credential storage. Three backends, in precedence
  order: `C1I_CLIENT_ID`/`C1I_CLIENT_SECRET` env vars (read-only), OS keyring
  (go-keyring), and a 0600 file under `os.UserConfigDir()` (fallback for
  headless Linux/CI/containers).
- `internal/login/` — OAuth device flow
- `internal/mcpgateway/` — JSON-RPC/streamable-HTTP client for the MCP
  gateway (backs `mcp gateway call`/`list-tools`)
- `internal/tokensource/` — OAuth2 token source

## Conventions

- Output: NDJSON for list/search commands, pretty JSON for single-object
  commands, plain text for auth.
- All list commands auto-paginate. `--page-token` disables auto-pagination.
- `docs` subcommands require no authentication.
- **Keep comments concise.** Whenever you write or change a comment, say the
  non-obvious thing and stop — a why, a constraint, or a gotcha the code can't
  express. Don't restate the code, recap how a bug was found, or narrate review
  history. Long comments drift and nobody updates them, so brevity is a
  maintenance property, not a style preference. This repo is also public, so a
  comment is published text — brevity limits both noise and the chance of
  leaking an internal tenant/hostname/ticket ID. Applies to comments you touch;
  don't go reformatting untouched ones.

### Global flags (persistent, on `rootCmd`)

- `--url` / `C1I_URL`, `--fields` / `C1I_FIELDS` (JSON field projection),
  `--max-retries` / `C1I_MAX_RETRIES` (default `client.DefaultMaxRetries`),
  `--error-format` / `C1I_ERROR_FORMAT` (`text`|`json`), `--dry-run` /
  `C1I_DRY_RUN` (preview a mutating request without sending it), `--debug` /
  `C1I_DEBUG` (trace HTTP requests to stderr). See README for behavior.

### Patterns to follow when adding/changing commands

- **ID arguments (enforced convention):** a command that addresses **one
  existing resource by its own id** takes that id as the **first positional
  argument**; the ids that merely **scope/parent** it (and everything else) are
  **flags**. Concretely:
  - Single-object read/mutate/action (`get`, `update`, `delete`, `approve`,
    `deny`, `comment`, `set-owner`, `resync-tools`, `source`, `usage`, …):
    the resource's own id is positional (`Use: "get <thing-id>"`,
    `cobra.ExactArgs(1)`, read via `args[0]`); parent ids stay flags
    (`--app-id`, `--connector-id` when it is a *parent*). Don't validate the
    positional id with `requireNonEmpty` — `cobra.ExactArgs` enforces presence;
    match the flat commands (`users get <user-id>`). **Presence is not
    non-emptiness:** `ExactArgs(1)` accepts `""`, `"/"`, or `"."`, and each
    used to reach the collection endpoint and print the whole list with exit
    0. The **shared REST client** now closes this two ways: it refuses any
    request whose path has an empty segment up front (`client.PathError` →
    exit 2, catches `""`), and it refuses to follow a 3xx whose target path
    differs from the request's (`client.RedirectError` → exit 2, catches
    `"/"`/`"."`, which the API 301s to the collection). A per-command check
    is still unnecessary *for commands built on it* — but don't reason as
    though a positional id were guaranteed non-empty or single-resource-
    shaped, and see "Adding a new client/subsystem package" below if your
    command doesn't go through that client.
  - A sub-resource or sub-list nested under **exactly one** owner id in the path
    (`/thing/{id}/…`) also takes that owner id positionally — e.g.
    `functions commits|usage|source <function-id>`,
    `mcp toolsets requestable-connectors <user-id>` (path
    `/users/{id}/…/requestable_connectors`).
  - **Flags** (never positional) for: searches / filtered or multi-scope
    collections (`list`, `search`, `/search/*` endpoints, and lists scoped by
    **more than one** id such as `mcp tools list --app-id --connector-id`);
    creates (`create`, `register`); and relationship/multi-id ops
    (`mcp bindings *`).
  - Never use a bare generic `--id` flag for a resource's own id — it must be
    the positional. Parent-scope ids keep descriptive flag names.
  This mirrors `users/apps/entitlements/automations/functions/requests get
  <x-id>`; the MCP and tasks commands follow the same shape
  (`mcp servers get <connector-id> --app-id`, `mcp tools get <tool-id> --app-id
  --connector-id`, `tasks approve <task-id>`). Keep README.md and `cmd/agents.md`
  in lockstep when this changes.
- **`Args` is optional, but if set it must match `Use` (enforced convention):**
  a runnable command need not declare `Args` — `attachSubcommandGuards`
  (`cmd/errors.go`) stamps `cobra.NoArgs` on any that leave it nil, so a stray
  positional is rejected with exit 2. Don't hand-add `Args: cobra.NoArgs` to a
  zero-positional command; the guard already covers it, and removing the guard
  would silently reopen the gap. If a command *does* take a positional,
  `Use` must document it (`<id>` / `[<id>]`) and `Args` must match that shape —
  `TestArgsUseConsistencyAcrossTree` (`cmd/args_positional_test.go`) walks the
  whole tree and fails CI on a mismatch. Exactly what it enforces: a required
  positional must be rejected one short of the documented count and accepted at
  it (no upper bound — `docs search <query>` deliberately uses
  `MinimumNArgs(1)`); an optional positional must declare non-nil `Args` that
  accepts 0 and the documented count; a command documenting none must not
  require one. Rejecting a *stray* positional is the guard's job, not the
  test's.
- **API client:** build it with `newClient(cmd, baseURL)` (cmd/client.go), not
  `client.New` directly — the helper threads the global flags (retries, etc.).
- **Paths:** interpolate IDs into request paths with `client.Path("…/%s", id)`,
  never `fmt.Sprintf` — `Path` URL-escapes each segment (a raw ID with `/`, `?`,
  `#`, or a space would otherwise mis-address the resource).
- **Output:** list rows go through the `newEmitter(...)`/`.Encode` emitter;
  single-object reads through `writeObject`; **mutation confirmations**
  (create/update/delete) through `writeRawObject` (never projected, so a
  session-wide `C1I_FIELDS` can't blank a success message). A `--fields` that matches
  no key anywhere in the result is a usage error (exit 2), enforced in two
  separate places: the list path counts matches across rows via rootCmd's
  `PersistentPreRunE`/`PersistentPostRunE`, and `writeObject` checks the
  single-object path inline (`projectionMatchedNothing`). Never add either hook
  to a subcommand — it silently disables the list-side check for that whole
  subtree (`TestNoSubcommandDefinesOwnPersistentPreRunE`,
  `...PostRunE`).
- **Row values keep their real JSON types.** A row is `map[string]any`: put
  `bool` and numeric values in as-is, never `strconv.FormatBool`/`Itoa`. NDJSON
  exists here so agents can pipe to `jq`, and stringifying breaks that
  silently — every non-empty string is truthy, so `jq 'select(.stable)'`
  matches `"false"`, and `jq 'select(.tool_count > 5)'` compares strings.
  This recurred across six row builders before it was caught. The same
  truthiness trap applies to absence: never emit `""` for a field with nothing
  to report, or `jq 'select(.field)'` matches every row. Either omit the key
  (`serverCountRow`'s `last_called_at`, `taskRow`'s `outcome`) or emit untyped
  `nil` (`policyRow`'s `deleted_at`) — follow the surrounding row's convention.
- **Errors:** the client returns typed `client.APIError` (carries status),
  `client.AuthError`, `client.PathError`, `client.RedirectError` (a 3xx is
  followed only when the path is unchanged AND the host is in the same trust
  scope — a followed hop is re-authenticated, so an unrestricted follow would
  leak the bearer token), and `client.RedirectLoopError` (a same-path chain
  that never settles in 5 hops); `internal/mcpgateway` adds `TransportError`
  for an unreachable gateway. `cmd/errors.go` maps them — plus its own
  `usageError`, `toolExecutionError`, `nonJSONResponseError`, and
  `upstreamError` wrappers, for failures with no natural client-level type —
  to exit codes:

  | Code | Meaning |
  |------|---------|
  | `0` | ok |
  | `1` | generic |
  | `2` | usage (bad flags/args, an empty id, a refused redirect, or API `400`) |
  | `3` | auth (`401`/`403`) |
  | `4` | not-found (`404`) |
  | `5` | rate-limited (`429`) |
  | `6` | C1 failed (API `5xx`, a redirect loop, or a `200` with a non-JSON body) |
  | `7` | tool-execution error (`mcp gateway call` result has `isError: true`) |
  | `8` | a system beyond C1 or the protocol layer failed (an unreachable gateway, an upstream connector failure, or a protocol-level JSON-RPC error) |

  Keep 6 and 8 distinct: 6 is worth retrying later, 8 usually is not. Wrap
  client errors with `%w` so `errors.As` can classify them, and wrap a bad
  flag/arg combination in `&usageError{}` so it exits 2 — a bare `fmt.Errorf`
  silently becomes exit 1.
- Retries (429/5xx + transport, idempotent-aware) live in the client
  (`internal/client/client.go`); commands get them for free via `newClient`.
- **Help text is a claim about the server.** Don't state a default, scope, or
  restriction in a `Long`/flag description you haven't seen the API honor. Quote
  the server's own error string when documenting a restriction so it's greppable
  from both directions. Four shipped examples of getting this wrong: two
  EXTERNAL-only `mcp servers` subcommands that documented no scope, a `--user-id`
  that promised "defaults to self" and 500s without it, and a `delete` that
  silently cascaded to every bound toolset's entitlement.
- **`api` is the escape hatch for endpoints with no first-class command.** It
  still goes through `newClient` and the output helpers, so retries and
  exit-code classification work as they do everywhere else; what it skips is
  `client.Path` escaping, since `--path` is raw caller input. If you find
  yourself documenting a raw `api` call for a common workflow, that's a missing
  command, not a documentation task.

### Adding a new client/subsystem package

A command built on the shared client (`newClient`) inherits the invariants above
for free. A **new package that talks to a remote service directly** (e.g. a
protocol client under `internal/`) does **not** — it must re-satisfy them
explicitly, and this is where they are most easily dropped. Before finishing such
a package, verify each of these against the new code:

- **Paginate to completion.** Any list/collection call must follow the API's
  cursor/next-page mechanism until it is exhausted and return the full set —
  never the first page only. Silent truncation reads as success.
- **Return typed, classifiable errors.** A non-2xx / auth failure must surface as
  (or wrap, with `%w`) an error type that `cmd/errors.go` maps to the exit-code
  taxonomy (3 auth, 4 not-found, 5 rate-limited, 6 C1 5xx, 8 upstream/protocol
  failure) — not a bare `fmt.Errorf`, which collapses to exit 1. A protocol
  client is exactly the case exit 8 exists for: keep "the service beyond C1
  failed" distinct from "C1 failed".
- **Escape ids in paths** (`client.Path`-style), use the shared output helpers in
  `cmd`, and honor the global flags where applicable.
- **Reject an empty id before sending, and don't trust a 3xx either.** The
  shared client refuses a path with an empty segment (`client.PathError` →
  exit 2) and refuses to follow a redirect whose target path differs from
  the request's (`client.RedirectError` → exit 2) — a package that issues
  its own HTTP inherits neither: `cobra.ExactArgs` will hand you `""`
  happily, and Go's default `http.Client` follows a 3xx transparently. An id
  of `""`, `"/"`, or `"."` reaching the collection endpoint by either path is
  the shape that produced the silent "returned the whole list with exit 0"
  bug.
- **Honor `--debug` and `--max-retries`.** Both are documented as global, and
  both are currently silently inert on the packages that issue their own HTTP —
  so tracing shows nothing and transient failures aren't retried on those paths.
  Don't add a fourth.

When implementing a wire protocol or stream parser (JSON-RPC, SSE, MCP, …), code
and test against the **full input space the spec permits**, not just the shape a
reference server happens to return today: multi-event streams, optional
fields/headers, paginated responses spanning multiple pages, and error payloads.
Add a test that drives the wired end-to-end path (e.g. via `httptest`), not only
the pure helper functions.
