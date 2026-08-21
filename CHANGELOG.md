# Changelog

All notable changes to c1i are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project aims
to follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **`functions usage` gained `--page-size`, `--page-token`, and `--limit`.**
  It already auto-paginated through every automation but had no way to opt
  out, unlike every other auto-paginating command. It now follows the same
  pattern as `functions commits`.

- **`deleted_at` on more list rows.** `apps`, `entitlements`, `connectors`,
  `functions`, `mcp tools`, and `mcp toolsets` list rows now carry
  `deleted_at`, and `grants list` carries `entitlement_deleted_at` and
  `app_user_deleted_at` for the objects behind a grant. The response structs
  did not declare the field at all, so the CLI was discarding what the API
  sent.

  For the six primary listings this is forward-looking rather than immediately
  useful: those endpoints exclude soft-deleted records, so the value is `null`
  in practice and there is no flag to request deleted rows. It matters on
  `grants list`, where a grant outlives the entitlement or account it points at
  — so `jq 'select(.entitlement_deleted_at)'` finds grants whose backing object
  is gone.

- **Every command now warns to stderr which tenant it's targeting when the
  URL came from `~/.c1i.yaml`.** Nothing on the command line names the
  config file, so a stale entry there used to send a command to the wrong
  tenant with no visible sign — this is the fall-through step in the
  incident that motivated this change: an agent exported `C1I_URL` in one
  shell call, lost it in the next, and silently got results from whatever
  tenant `~/.c1i.yaml` named. `--url` and `C1I_URL` don't print this warning
  — both are an explicit choice for that invocation, and warning on every
  normal `C1I_URL` use would just train people to stop reading it. The
  warning is stderr-only (stdout stays clean NDJSON/JSON) and prints once
  per invocation, not once per request in a paginated list.

### Changed

- **BREAKING — `mcp servers search` no longer reports `tool_count: 0` when
  `--tool-state` is omitted.** The API only computes a per-server tool count
  when the request carries a `tool_state` filter; a filterless request never
  runs the count and leaves the field at its unset zero value. The CLI was
  surfacing that unset zero as `tool_count: 0`, indistinguishable from a
  server that genuinely has none — live-verified on a server with 729 tools
  (47 approved, 664 pending, 18 removed) that reported `tool_count: 0` by
  default and the correct `47` with `--tool-state approved`. `tool_count` is
  now omitted entirely unless `--tool-state` is passed; a `jq 'select(.tool_count
  == 0)'` pipeline no longer matches every filterless row regardless of how
  many tools a server actually has. Passing `--tool-state` is unaffected,
  including a genuine 0 for a state with no matching tools.

- **BREAKING — no `--url`, `C1I_URL`, or `~/.c1i.yaml` at all now exits `2`
  (usage), not `1` (generic).** A missing URL is a missing required argument,
  the same class of problem `requireNonEmpty` already maps to `2` for a
  missing flag — this path returned a bare error instead of the `usageError`
  wrapper, so it fell through to `1`. Message text is unchanged, only the
  exit code.

- **BREAKING — a wide sweep of bad-flags/args conditions now exits `2`
  (usage) instead of `1` (generic), and every 4xx API status other than
  401/403/404/408/429/499 now exits `2` instead of `1`.** Both are the same
  underlying defect: a bare `fmt.Errorf` (or an unlisted HTTP status)
  falling through `cmd/errors.go`'s `exitCode()` to the generic code instead
  of the documented usage one. Affected commands include `auth login`
  (`--client-id` without `--client-secret`, no URL configured at all),
  `api` (malformed `--body`/`--body-file` JSON), `functions list`
  (`--published-only`/`--draft-only`), `mcp bindings
  create|delete|by-tools|history` (empty/conflicting `--tool-id`/
  `--toolset-id`), `mcp servers update|update-credentials|test-connection`
  and `mcp toolsets update` (nothing to update, an invalid `--type`/`--auth`,
  a missing config, mutually exclusive config flags), `mcp tools approve
  --state removed`, `mcp gateway list-tools|call` (an underivable gateway
  URL), `docs endpoint` (an unknown path), and the "could not determine the
  current user/policy step, pass the flag explicitly" guards in `requests
  create-grant|create-revoke|list`, `tasks list --assigned-to-me`, and `tasks
  approve`. The 4xx rule is a status-range check (`400 <= code < 500`, minus
  the five already-classified codes), not a per-status list, so it also
  covers 409/413/414/422 and any future status the API adds without another
  code change.

  `408` and `499` are carved out of the range rather than swept in: both mean
  the request ended without the caller having done anything wrong, so
  neither is a usage error (`2`); and neither is evidence that C1 itself
  failed, so neither is `6` either — both fall to the generic `1` instead.
  This API has been observed to produce both, from different error paths.
  `425` has no such observation behind it and stays in the usage range on
  that basis, not by omission. Neither `408` nor `499` can be forced
  deterministically against the live API, so both are proven by unit test
  only. Message text is unchanged everywhere; only the exit code.

- **BREAKING — more list rows emit real JSON numbers and `null`, not strings.**
  The same fix as the earlier stringified-values change, applied to the fields
  it missed. `apps list`'s `user_count` and `entitlements list`'s `grant_count`
  were quoted strings, and `jq` orders every string above every number, so
  `select(.grant_count > 5)` matched **every** row — 3000 of 3000 on one
  tenant, where 146 qualify. They are now numbers. Absent values that came out
  as `""` are now `null`: `grants list`'s `deprovision_at`, `automations
  list`'s `last_executed_at`, `automations executions list`'s `completed_at`,
  `functions list`'s `published_commit_id`, and `mcp servers connections
  list`'s `connected_at`, `authorized_as_email`, and `authorized_as_name`. `""`
  is truthy in `jq`, so a presence filter over those matched every row too —
  579 of 579 grants, none of which had one set.

  Key names and ordering are unchanged; only value types. A consumer comparing
  these against strings, or testing an absent value with `== ""`, must switch
  to the native types — and note that `jq -r` now prints `null` where it
  printed an empty line.

- **BREAKING — a list row (or `api --paginate` item) whose `--fields`/
  `C1I_FIELDS` projection matches nothing is no longer printed.** Each row was
  projected independently as it streamed, so a bogus field name still wrote
  one `{}` per row before the whole-result zero-match check (further down the
  pipeline) turned it into exit `2` — a consumer piping stdout without
  checking the exit code saw only syntactically valid, semantically empty
  JSON and no signal anything was wrong; live-verified as 46 lines of `{}`
  from `apps list --fields <typo>`, still exit `2`. A row that projects to
  `{}` is now skipped instead of written; the zero-match exit `2` is
  unchanged (stdout is simply empty when nothing matched anywhere), and a
  field present on only some rows now prints only the rows where it's
  actually present. A consumer counting output lines for a sparse `--fields`
  spec will see fewer lines than before.

  `--limit` now also counts rows actually **written**, not rows scanned, so
  it composes correctly with a sparse `--fields`: previously a filtered-out
  (skipped) row still counted against `--limit`, so `--limit N` combined
  with a sparse field could stop pagination having written fewer than `N`
  lines, or before a later page's real matches were ever fetched —
  live-verified as `tasks list --fields outcome --limit 5` returning only 4
  lines pre-fix, 5 post-fix. Combining `--limit` with a sparse `--fields`
  may now fetch further pages than before, since more pages can be needed
  to actually reach `limit` written rows; `--limit` alone, or `--fields`
  alone, is unaffected.

  The per-page request size stays at the requested `--page-size` throughout
  such a scan; it no longer collapses toward `--limit` (as small as a
  handful of rows per request instead of the intended page size) just
  because matches are sparse — that collapse would have made the "fetch
  more pages" tradeoff above far more expensive than it needs to be, turning
  a few full-sized pages into many mostly-wasted small ones.

- **`functions source --out-dir` writes files `0600` instead of `0644`**, and
  the directory `0700` instead of `0755` — now including a pre-existing
  directory, not just one this command creates. Fetched function source is
  developer-authored code, and code commonly inlines credentials — API keys,
  webhook secrets, third-party tokens. The CLI cannot tell whether a given
  function's source does, so it no longer writes it group- or world-readable.
  `0700` rather than a looser `0750`: the files inside are already `0600`, so
  group `r-x` on the directory buys nothing against this threat model — a
  group member could never read file content, only list filenames (which can
  themselves be informative, e.g. `stripe-webhook-secret.ts`) and stat
  metadata. `--out-dir` pointed at an existing, more permissive directory
  (e.g. a script's own prior `mkdir dir && chmod 777 dir`) is tightened,
  with a warning to stderr naming the old mode — never silently, and never
  loosened if it was already stricter than `0700`. Any setuid/setgid/sticky
  bit on the directory is stripped outright rather than preserved, even if
  the permission bits alone didn't need tightening: at `0700` there is no
  group or other access left, so none of the three bits have any effect to
  preserve. Anyone relying on the old modes, or on that directory staying
  wide open, will need to widen it back deliberately after running this
  command.

### Fixed

- **`c1i docs agents` rendered a doubled `v` in its header**, e.g.
  `(vv0.4.1-...)`. The template read `(v{{VERSION}})`, but `Version` (from
  `debug.ReadBuildInfo()`) already carries its own leading `v`. Cosmetic, but
  `cmd/agents.md` is `go:embed`-ed, so it shipped inside every binary.

### Security

- **`gosec` and `gitleaks` now gate CI**, alongside the vulnerability scan.
  Both cover classes the existing gates do not: `gofmt`, `go vet`, and
  `golangci-lint` all pass on a committed credential. Findings are triaged with
  per-line `#nosec` comments carrying a verified reason, never a blanket rule
  exclusion, and the gate rejects a bare or unjustified annotation so that
  cannot quietly become the norm.

- **The v0.4.0 and v0.4.1 binaries contain reachable standard-library
  vulnerabilities; upgrade rather than continuing to run them.** They were
  built with Go 1.25.7, and a release binary embeds the standard library, so
  the vulnerability ships inside the artifact. Scanning as that toolchain
  reports 14 reachable on Linux and 13 on macOS and Windows, across `net/url`,
  `net/http`, `crypto/tls`, `crypto/x509`, `encoding/asn1`, `net/textproto`,
  `net`, and `os`. This release builds with Go 1.26.7 and upgrades `x/text` and
  `x/sys`; `govulncheck` now reports none, and CI and the release build both
  gate on it, scanned per platform because reachability is build-tag dependent.

## [0.4.1] - 2026-08-20

### Fixed

- **`c1i version` reported `v0.4.0+dirty` on the 0.4.0 binaries.** The version
  comes from version-control state at build time, and the release build's own
  `go mod tidy` step rewrote `go.mod` before compiling — so every binary was
  stamped as built from a modified tree. `go.mod` listed `spf13/pflag` as an
  indirect dependency while the code imports it directly. Only the reported
  string was wrong: the binaries were otherwise correct and their checksums
  verified. CI now fails when `go mod tidy` changes anything, so a release
  build is no longer the first thing to notice.

## [0.4.0] - 2026-08-20

### Added

- **`policies list|get|search|create|update|delete|validate-cel`** — first-class
  commands for C1 policies (approval/provisioning/certification workflows).
  `create`/`update` refuse client-side (exit 2) to send a request with
  empty/missing steps for a policy's baseline entry — `POST /api/v1/policies`
  with no `policySteps` silently succeeds and returns a deny-everything
  policy (a single `{"reject":{}}` step), with no validation error; pass
  `--allow-deny-all` if that's genuinely intended. They also refuse an
  unspecified `--policy-type`, an empty `rules[].condition`, a `provision`
  step, a step with none of its `approval`/`provision`/`accept`/`reject`/
  `wait`/`form` arms set, `fallback`/`fallbackUserIds` on an approver arm
  that doesn't support them, `fallback:true` with nothing to fall back to,
  and the `agent` approver arm's own rules (a required `agentMode` and
  `agentFailureAction`; agent steps only in grant/certify policies;
  comment-only mode in a certify policy; `policyIds` when the mode is
  change-policy; `reassignToUserIds` when the failure action reassigns) —
  most of these are bare server errors that otherwise surface as an opaque
  `HTTP 500` rather than a `400`. `update` builds the API's required
  `{"policy": {...}, "updateMask": "..."}` wrapper for you (a flat body
  400s). `validate-cel` checks a CEL condition (root variable `subject`, not
  `user`) without creating or updating anything, and exits 2 on a condition
  that does not compile so `validate-cel '<cond>' && ...` is safe to script.
  `list`/`search` rows carry `deleted_at` — `null` on a live policy, so
  `jq 'select(.deleted_at)'` selects only soft-deleted ones. See README's
  "Policies" section for the full flag surface and guard list.
- **`docs agents [-o FILE]`** — print a short, agent-facing bootstrap doc (no
  auth required, like the other `docs` subcommands): tenant/auth, output
  contracts, exit codes, pagination, and when to prefer a first-class command
  over `c1i api`. It does not enumerate every command or flag — use the cobra
  command tree (`c1i --help`, `c1i <group> --help`) for that, since it can't
  drift out of sync with the binary the way a static doc can. Root and `docs`
  help now point agents at it first, instead of framing the goal as finding
  "the right API calls."
- **`api --allow-delete-body`** — explicit opt-in that lets `--method DELETE`
  carry a `--body`/`--body-file`. Some C1 endpoints are body-taking DELETEs
  (e.g. `.../remove-membership`, which needs `{"appUserId": "..."}` to say
  which membership to remove), and without an opt-in they were completely
  uncallable through `c1i api` — the documented escape hatch for exactly this
  case. The default is unchanged: `--method DELETE --body` without the flag
  still refuses with the same error (now also naming the opt-in), so the
  guard still catches an accidental body on an ordinary DELETE. `--dry-run`
  previews the method, path, and body when the opt-in is set.
- **`docs guide [name]`** — print an embedded, task-oriented runbook (no auth
  required, like the other `docs` subcommands). Run with no argument to list
  the available names. Ships with `register-mcp-server`, `assign-toolset-
  everyone`, `test-mcp-gateway` (a pre-flight checklist for verifying a
  server's tools are approved, entitled, and served through the gateway), and
  `delegate-entitlement-provisioning` (an entitlement "proxy binding" alone
  grants nothing — this walks through the required second step,
  `provisionerPolicy.delegated`).
  Content is embedded as Go string constants — no network call, unlike
  `docs search` / `docs page`.
- **`docs guide` gains three app/access-request runbooks:
  `configure-new-app`, `request-access`, and `inspect-and-approve-task`.**
  `configure-new-app` stands up a manually-managed app, sets its owners, and
  creates a custom entitlement for it via the 3-call resource-type/resource/
  entitlement sequence (no first-class `entitlements create` exists).
  `request-access` walks the requester side of a grant/revoke request —
  finding a real app/entitlement/user, previewing with `--dry-run`, filing
  the request, and verifying the resulting grant — and notes that real
  requestability is decided by catalog membership, not an entitlement's
  `grantPolicyId` (a weak signal either way). `inspect-and-approve-task` is
  the single source for approver-side mechanics: reading a task's embedded
  policy (`stepApproverIds`, the `actions` gate, `policy.current`/`.next`),
  commenting (unlike approve/deny, not gated the same way), and the
  approve/deny step-resolution asymmetry (approve requires a resolvable
  current step; deny proceeds without one).
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
- **`apps delete <app-id>`** — soft-delete an app via `DELETE /api/v1/apps/{id}`
  (sets `deletedAt`, retained for audit). Complements `apps create` so a
  container app made by mistake can be cleaned up without the raw `api` escape
  hatch. Honors `--dry-run`.
- **`apps set-owners <app-id> --user-id …`** — set an app's owner list via
  `PUT /api/v1/apps/{id}/owners` (replaces the full set; `--user-id` repeatable).
  Owner provisioning is asynchronous, so the command notes that new owners take
  ~60-90s to appear in `apps get`; a success means the request was accepted.
  Honors `--dry-run`.
- **`apps set-owners --wait` / `--wait-timeout`** — optionally block after the
  `PUT` and poll `GET /api/v1/apps/{id}/ownerids` (every 12s, default timeout
  4m) until every requested `--user-id` shows up as provisioned, printing
  progress as it goes. On timeout, exits non-zero with a message clarifying
  that provisioning is still pending and not necessarily a failure — owner
  provisioning has been observed to take from under two minutes up to several.
  Without `--wait`, behavior is unchanged; `--wait` combined with `--dry-run`
  still only previews the `PUT` and never polls.
- **`mcp gateway list-tools` / `mcp gateway call`** — drive the C1 MCP gateway
  over its streamable-HTTP MCP transport (the same handshake an MCP host does:
  initialize → notifications/initialized → tools/list / tools/call), closing the
  configure-then-verify loop so you can list and invoke a registered server's
  tools without hand-rolling the protocol. The gateway URL is derived from
  `--url` (inserting `-mcp` into the host) or set with `--gateway-url`; your
  standard C1 token is accepted, so no extra auth setup is needed. `list-tools`
  emits NDJSON (`--full` adds each tool's input schema); `call <tool> --args
  '{…}'` prints the tool result.

### Changed

- **BREAKING — c1i requires `https`; another scheme is rejected, not rewritten.**
  A non-`https` URL used to be silently rewritten to `https://<host>` with a
  warning, so a caller who typed `http://` got a request they did not ask for.
  It now exits `2` naming the scheme. There is no plain-`http` path and no
  override — a bearer token is never sent over a scheme the caller did not ask
  for.
  This also removes advice that never worked: the bare-name error previously
  suggested an explicit scheme "e.g. `http://localhost:8080`" for local
  development, which the rewrite had always defeated.
  A single-label host still works with an explicit scheme (`https://c1-staging`),
  which is how an internal-resolver hostname is reached.

- **BREAKING — a bare tenant name is no longer expanded to a domain.**
  This changes exit codes for anything branching on them: `--url mycompany`
  used to become `https://mycompany.conductor.one` and proceed, so a script saw
  exit `0` when that guess happened to work — or a downstream `3`/`4`/`6` when it
  did not. It now exits `2` deterministically, before any request is sent.
  With a second tenant domain family in use (`*.c1eu.ai`, for EU tenants) the
  expansion was ambiguous, and it silently pointed an EU tenant at a US host —
  a confusing auth failure, or worse, a different real tenant. The error names
  where the value came from: the `--url` flag, `C1I_URL`, `~/.c1i.yaml`, or the
  interactive login prompt. That matters most for a stale entry in the config
  file, where nothing on the command line mentions it. Pass a full host instead
  — `mycompany.conductor.one` or `mycompany.c1eu.ai`. A bare `localhost` is
  rejected too, where it previously became the meaningless
  `https://localhost.conductor.one`.
  Stored credentials for `*.conductor.one` tenants are unaffected: both the
  primary and legacy keychain keys derive from the resolved URL, and the retired
  shortcut resolved to exactly what the full host resolves to, so no
  re-authentication is needed. An EU tenant never had a credential reachable
  through the shortcut, which only ever expanded to `.conductor.one`.

- **New exit code `8` for a failure beyond C1, so `6` no longer covers an
  upstream failure.** **Breaking change for anything branching on exit codes:**
  exit `6` previously covered two situations that call for opposite responses:
  C1 itself failing, and an upstream MCP connector failing. An agent could not
  tell "C1 is down, wait and retry" from "the Slack connector is down, retrying
  won't help." What moved to `8`: an upstream connector failure (a JSON-RPC
  error carrying code `0`), and protocol-level JSON-RPC errors — `-32601`
  (method not found), `-32700` (parse error), `-32600` (invalid request).
  `-32601` moved *from* exit `2`: this client only ever sends four fixed,
  spec-required methods, so a caller cannot cause "method not found" — it means
  a protocol mismatch or a bug here, and exit `2` sent people hunting their own
  command line. A test now pins that method set, so adding an outbound method
  fails the build rather than silently invalidating the reasoning.
- **BREAKING — a JSON-RPC error object carrying no `code` field now exits `1`, not `6`.**
  The field was a plain `int`, so an absent code decoded to `0` — indistinguishable
  from a genuine code `0`, and therefore misreported as an upstream connector
  failure. Presence is now tracked, so "no code" is generic and unclassified,
  which is what it is.
- **BREAKING — a bare `400` from the API now exits `2` (usage), not `1`.** Most `400`s here
  are a value the CLI forwarded without local validation — a bad page token, an
  out-of-range page size, a misspelled enum, a malformed id — and for those,
  exit `2` is right and no retry will help. Be aware of the limit, though: some
  `400`s are state or business-rule rejections rather than bad input. Approving
  an already-closed task returns `400 task is closed`, and that now reports exit
  `2` even though nothing about the invocation was wrong. Exit `2` on an API
  `400` therefore means "the server rejected this request outright" — read the
  message before concluding your flags were wrong. Mapping `400` to the nearest
  existing bucket was a deliberate simplification rather than adding per-status
  carve-outs. `409` and other unlisted statuses still exit `1`.
- **BREAKING — NDJSON rows emit real JSON booleans and numbers, not strings.**
  List commands stringified every non-string row value, so `stable` came out
  as `"true"` and counts as `"7"`. That silently broke the documented reason
  NDJSON exists here — piping to `jq` — because a non-empty string is always
  truthy: `jq 'select(.stable)'` matched entries whose value was the string
  `"false"`, and `jq 'select(.tool_count > 5)'` compared strings, not numbers.
  Now `stable` and `connected` are `true`/`false`, and `tool_count`,
  `required_scope_count`, `optional_scope_count`, and `grant_source_count` are
  bare numbers. Key names and ordering are unchanged; only value *types*
  changed. A consumer comparing these against the strings `"true"`/`"false"`
  or `"7"` must switch to the native types. `--fields` projection is
  unaffected (it already round-tripped rows as `map[string]any`).
- **BREAKING — ID arguments are now positional.** A command that addresses one
  existing resource by its own id now takes that id as the **first positional
  argument** instead of a flag; parent/scope ids remain flags. This makes the
  whole CLI consistent (flat commands like `users get <user-id>` already worked
  this way). Migrate:
  - `mcp servers get\|update\|update-credentials\|delete\|resync-tools --connector-id X --app-id A`
    → `mcp servers <verb> X --app-id A` (and `test-connection` takes `[<connector-id>]` positionally).
  - `mcp tools get\|approve\|delete\|history --id X --app-id A --connector-id C`
    → `mcp tools <verb> X --app-id A --connector-id C`.
  - `mcp toolsets get\|update\|delete --id X …` → `mcp toolsets <verb> X …`;
    `mcp toolsets get-by-entitlement --app-entitlement-id X --app-id A` → `… X --app-id A`;
    `mcp toolsets requestable-connectors --user-id X` → `… X`.
  - `tasks approve\|deny\|comment --task-id X` → `tasks <verb> X`.
  - `accounts set-owner --app-user-id X --app-id A --user-id U` → `accounts set-owner X --app-id A --user-id U`.
  Collection (`list`/`search`), create, and relationship (`mcp bindings *`)
  commands are unchanged (their ids stay flags). The old flags now error with
  "unknown flag".
- **Ctrl-C now cancels cleanly.** The root command wires `cmd.Context()` to a
  `signal.NotifyContext` on SIGINT/SIGTERM, so a long-running command (e.g.
  `apps set-owners --wait` polling for async owner provisioning) sees its
  context canceled and can exit with a clear message instead of the process
  being hard-killed with no output. A first Ctrl-C cancels gracefully; a
  second reverts to the OS default hard-kill.
- **`mcp servers catalog list`** rows now include `base_url`, `default_tool_prefix`,
  `stable`, `required_scope_count`, and `optional_scope_count` (in addition to
  the existing fields, kept as-is). The catalog holds many near-duplicate
  entries for the same service — a thin REST wrapper (`slack`, base_url
  `https://slack.com/api`) alongside the vendor's own hosted MCP endpoint
  (`slack-mcp`, base_url `https://mcp.slack.com/mcp`) — and `display_name` /
  `service_name` alone didn't reliably tell them apart. `required_scope_count`
  / `optional_scope_count` summarize each entry's OAuth scope tiering, which
  turns out to live per auth mode (`authModes[].scopes` vs `.optionalScopes`)
  rather than as a single catalog-wide list; the entry-level `defaultScopes`
  field some assumed carried it is empty on every catalog entry seen in
  production. `mcp servers catalog get --help` documents the details.
- **BREAKING — an empty required flag value** (e.g. `--app-id ""`) now exits `2` (usage),
  matching a missing required flag, instead of `1` (generic). The check
  (`requireNonEmpty`) applies this consistently across every command that uses
  it, so automation branching on exit codes sees a stable usage signal.
- **BREAKING — `mcp gateway call` now exits `7` when the tool itself fails.**
  Previously, a tool result with `isError: true` (the tool ran, but reported
  its own failure — e.g. a timed-out deployment) exited `0` like a success,
  because nothing inspected `isError`; only a transport/protocol failure (a
  non-2xx HTTP status, or a JSON-RPC `error` response) was ever non-zero. The
  new exit code `7` is distinct from the existing 3/4/5/6 transport codes —
  it means "the call completed but the tool reported an error," a different
  failure class entirely. **The full result is still printed to stdout
  exactly as before**, `isError` and all, so an in-band consumer (e.g. an
  LLM host reading the error text out of the `content` array) is unaffected;
  only the process exit code changes.

### Removed

- **`cmd/skill.md` / `docs_skill.go`** — retired into `cmd/agents.md` /
  `docs agents`. An audit found the bulk of `skill.md` was either redundant
  with `c1i <cmd> --help` or a raw-REST manual with no equivalent guidance
  toward first-class commands; per-resource NDJSON field-name tables were
  dropped outright (they drift on every schema change and nothing validated
  them). `docs skill` is kept as a cobra alias of `docs agents` — both emit
  identical output — so existing scripts and habits keep working.

### Fixed

- **An id of `/` or `.` no longer returns the whole collection with exit `0`.**
  The empty-id guard added previously inspects the request this CLI builds, but
  `/` escapes to `%2F`, and the API redirects that to a trailing-slash path — the
  exact shape the guard refuses — and then to the bare collection. Go's HTTP
  client follows redirects below the guard's layer, so `users get "/"` printed
  every user and reported success. The REST client now **refuses to follow any
  redirect** and reports the `3xx` and its target as a usage error (exit `2`),
  because following one silently converts a single-object read into a collection
  read. A redirect is followed only when the path is identical (a trailing-slash
  difference counts as a change) AND the target host is in the same trust scope —
  identical modulo scheme/port, or a `label.`-prefix relationship with at least
  two labels, covering `apex ↔ www`. The host restriction is a security boundary,
  not tidiness: a followed redirect is re-authenticated, so following one to an
  arbitrary host would hand the caller's bearer token to whatever the `Location`
  named. A chain that doesn't settle within five hops fails as exit `6`.
  Verified with `--debug` that normal calls perform zero redirects today and are
  unaffected. This lives in `internal/client`; `internal/mcpgateway` and
  `internal/login` build their own HTTP clients and still follow redirects.
  Exit `2` is a deliberate simplification: a bad id is the only cause of a `3xx`
  seen so far, but a redirect on an otherwise well-formed request would not be
  the caller's mistake and would still report `2`.
- **An unreachable MCP gateway now exits `8`, not `1`.** A DNS failure or a
  refused connection during the gateway handshake returned a bare error that
  collapsed to generic. Exit `8` already means "a system beyond C1 failed",
  which is exactly this; the earlier work classified JSON-RPC response codes and
  never reached the transport path, which fails before any JSON-RPC body exists.
  A rejected credential still exits `3` and a real C1 `5xx` still exits `6`.
- **`api` no longer exits `0` printing a non-JSON body.** A `--path` that escapes
  the API prefix can reach the web app, which answers `200` with an HTML
  document; `c1i api` printed it verbatim and reported success, so a downstream
  parser broke with no signal. A `200` whose body is not JSON is now an error
  (exit `6`). Exit `6` rather than `1` because the failure is known, not
  unclassified: the remote replied, just not with the JSON contract `api`
  promises — the same "replied but not usefully" case `6` already covers for
  `5xx`, wearing a success status code. An empty body still succeeds, since some
  endpoints answer a write with nothing.
- **`--url` is normalized instead of silently coerced.** The host is now
  lower-cased, so `HTTPS://TENANT.CONDUCTOR.ONE` works — it previously failed as
  "not authenticated", because the credential key was case-sensitive while DNS
  and HTTP hosts are not. A protocol-relative `//tenant.example` is handled
  rather than mangled into `https:////tenant.example`. Credentials embedded in
  the URL are dropped with a warning on stderr instead of silently; the password
  is never echoed.
  **If you previously authenticated with a mixed-case `--url`**, that credential
  was stored under the old exact-case key and is no longer found. Run
  `c1i auth login` once to re-store it.
- **`api --path` without a leading slash says so.** It concatenated onto the
  host, producing `https://tenant.conductor.oneapi/v1/users` and a DNS error that
  never named the real problem. Now a usage error (exit `2`) quoting the path.
- **The `policies --steps-file` guard rejects a non-object step.** A string,
  number, array, or `null` inside `steps[]` was silently skipped by the arm check
  and reached the server, which answered with a raw protobuf parse error. Each is
  now refused client-side (exit `2`) naming the JSON kind it found.
- **An argument or flag value that isn't valid UTF-8 is refused client-side.**
  It reached the server and returned a bare `500`, reporting a caller mistake as
  a remote failure (exit `6`). Now exit `2`, checked once for every command and
  every flag rather than per command. A hostile-but-valid-UTF-8 id still goes to
  the server and gets its normal `400`. Note the check is uniform, so a file path
  containing invalid UTF-8 — legal on Linux, though unusual — is also refused;
  requiring UTF-8 arguments is deliberate rather than maintaining a list of which
  flags are exempt.

- **An empty id argument no longer returns the whole collection with exit `0`.**
  `c1i users get ""` (and `apps`, `policies`, `functions`, `automations`) printed
  the full `{"list":[...]}` and reported success: `cobra.ExactArgs(1)` counts `""`
  as an argument, the empty id rendered a trailing empty path segment, and the
  API redirected that to the collection endpoint. The shared REST client now
  refuses any request whose path carries an empty segment — before anything is
  sent — and exits `2`. It is one check at the point every REST request passes
  through, so every command built on that client is covered, including a raw
  `api --path /api/v1/policies/`. It does **not** cover the three subsystems
  that issue HTTP themselves (`internal/tokensource`, `internal/login`,
  `internal/mcpgateway`); none of those builds a path from a caller-supplied id
  today, but `mcp gateway call` takes a positional argument and is guarded by
  its own JSON-RPC validation rather than by this check.
  **Breaking change for anything branching on exit codes:** `tasks approve ""`
  and `tasks deny ""` previously exited `4` (that endpoint answers `404` rather
  than redirecting); they now exit `2`, which correctly classifies an empty
  argument as caller error rather than "not found".
- **The refusal message no longer claims to be an API error.** It read
  `Error: API error: refusing to send GET /api/v1/policies/: …`, which was a
  false claim — no request reached the wire. Prefixes inherited from call sites
  are now dropped for this error in both text and `--error-format json` output.
  Every other error keeps its full context.
- **`--fields` on list commands and `api --paginate` now errors (exit `2`)
  when the spec matches nothing anywhere in the result, instead of silently
  printing an empty `{}` per row and exiting `0`.** Single-object `get`
  commands already treated a total miss as a usage error; list output went
  through a different code path (the NDJSON emitter) that never checked, so
  `c1i users list --fields totally.bogus.path` looked like "zero users
  matched" instead of "your field path is wrong" — an agent reading exit `0`
  plus empty rows as "no data" rather than "bad flag" could burn a couple of
  guesses before thinking to drop `--fields` and look at the real shape.
  Fixed via a single central hook (`rootCmd`'s `PersistentPostRunE`) rather
  than a check repeated at every list command's call site, so no future list
  command can add itself without the guard; a tree-walk test fails CI if any
  subcommand ever defines its own `PersistentPostRunE`/`PersistentPreRunE`
  and silently disables it. The rule matches the single-object case exactly:
  it's a *zero-match* check over the **whole result**, not per-row — a field
  present on some rows and absent on others still exits `0` (sparse data is
  expected, not an error), and rows are always streamed out as they're
  fetched, never buffered, so a paginated result's rows are fully printed
  before the error is returned. Note for anyone piping output
  (`c1i ... --fields ... | jq ...`): `$?` after a pipe reads the pipe's *last*
  command, not `c1i`'s, so the stderr message is the durable signal to check
  for, not a naive `$?`.
- **A credential stuck in an unreachable OS keyring was misreported as "no
  credentials found: run `c1i auth login`."** `keychain.Load` falls back to
  its 0600 file store both when the keyring has never seen a credential and
  when the keyring is merely unreachable (headless Linux/containers without
  a D-Bus session bus) — but it discarded that distinction once it decided to
  fall through, so a real credential sitting inert in the keyring looked
  identical to never having logged in. Following the old advice made things
  worse: it wrote a second, file-backed credential while the original
  keyring entry stayed unreachable. Load now says plainly that the file
  store is empty *and* the keyring is currently unavailable, names the
  underlying keyring error, and notes that `auth login` will store a new
  credential in the file store rather than recover the keyring one. The
  genuinely-never-logged-in message is unchanged, and this still surfaces as
  `client.AuthError` (exit 3), same as before.
- **The `docs guide` drift guard (`TestGuideCommandsResolveAgainstCobraTree`)
  now validates positional-argument counts, short flags, and single-quoted
  values, and flags "c1i ..." text it can't check.** An audit of the guard
  found it only checked `--flag` names, so a guide invocation with a missing,
  extra, or unregistered short flag passed the guard while the real binary
  exited 2. It now resolves each invocation's Args validator against the
  positionals actually left over (distinguishing them from flag values,
  including inline `--flag=value`/`-fvalue` forms), checks `-f` shorthand
  flags via `ShorthandLookup`, and tokenizes single-quoted values (previously
  only double quotes were tracked, so a single-quoted JSON value containing
  a space and a literal `--` was misread as an unregistered flag). It also
  now fails on a `c1i ...` mention that sits outside the three shapes it
  extracts (an unquoted mid-sentence reference, or a line not starting with
  `c1i`) when that mention carries a `--flag`-shaped token, rather than
  silently skipping it.
- **`docs guide register-mcp-server` and `mcp servers test-connection --help`
  both described the response fields in the wrong case.** Both documented
  `tool_count` / `failure_reason`; the live response is camelCase
  (`toolCount`, as a string, and `failureReason`), verified against a test
  tenant. `register`'s and `resync-tools`' help text make no comparable
  output-field claims, so neither needed a matching fix.
- **`README.md`'s `mcp servers test-connection` line had no HOSTED-vs-EXTERNAL
  annotation**, inconsistent with `cmd/skill.md` and with README's own
  `resync-tools` line. Both README lines now note `# EXTERNAL only; 400 on
  HOSTED`.
- **`docs endpoints --filter` now names the known hidden endpoint families
  instead of only pointing at `docs search`.** A filter that matches nothing
  used to say only "try `docs search`," which reads as "this endpoint
  doesn't exist" for the endpoints that are real, work live, and are simply
  absent from the public OpenAPI spec on purpose. The miss message now names
  the two families verified against the live public spec and a live C1
  tenant — the MCP admin surface (`mcp_servers`/`mcp_tools`/`mcp_toolsets`, covered
  by `mcp servers`/`mcp tools`/`mcp toolsets`) and access reviews
  (`access_review`/`access_reviews`, reachable via `api --path`) — with the
  concrete next step for each, before falling back to `docs search` for
  anything else. Two other candidate "hidden" families from the same
  customer report turned out not to fit this message: `bundle_automation` is
  already in the public spec (`docs endpoints --filter bundle_automation`
  finds it), and `managed_state_bindings` isn't a standalone endpoint at all
  — `AppManagedStateBindingRef` is a request-body field on the existing
  `connectors` (`CreateDelegated`) endpoint.
- **`mcp bindings --help` now disambiguates itself from entitlement proxy
  bindings.** `mcp bindings` operates on MCP tool↔toolset bindings only. A
  customer's agent, grepping c1i for "bindings," had no signal that the
  entitlement→entitlement "proxy binding" object (used for delegated
  provisioning) is a different thing entirely that c1i has no dedicated
  command for. The command's `--help` now states both explicitly and points
  at the new `docs guide delegate-entitlement-provisioning` runbook.
- **`entitlements --help` now documents that some system-builtin
  entitlements share a canonical ID across apps.** Verified live against a
  test tenant: the base "Access" entitlement carries the identical id
  (`287oY0rG4UirjDNFEYguMBvxyim`) on GitHub, Salesforce, Bitbucket Cloud,
  Snowflake, and Google Workspace apps alike (the same pattern observed for
  MCP's "All approved tools"/"Read tools" system toolsets). A
  customer's agent lost real time ruling out data corruption before
  realizing this was intentional. `--help` now states it and calls out that
  an entitlement id is only unique per app — always key on (app-id, id).
- **`login` failures now use the exit-code taxonomy.** The OAuth device-flow
  helpers returned bare errors for non-2xx responses from the device, token, and
  personal-client endpoints, so every login failure collapsed to exit `1`
  regardless of cause. They now surface a typed `client.APIError` (mapping the
  status to `3` auth / `5` rate-limited / `6` server) or, for an OAuth
  `access_denied`/`expired_token` response, a `client.AuthError` (exit `3`) — so
  automation wrapping `c1i login` sees the same stable signals as the rest of the
  CLI.
- **`--fields` now bridges snake_case/camelCase.** List commands emit rows in
  snake_case while single-object reads emit camelCase, so `--fields displayName`
  on a list command silently returned `{}`. Field matching now falls back to a
  case- and separator-insensitive comparison when an exact key match misses, so
  a projection in either style resolves against either output. Exact matches are
  unchanged (they always win), and the output keeps the source key spelling —
  `--fields` selects keys, it never renames them.
- **`--fields` on a single-object `get` no longer silently returns `{}` with
  exit `0`.** Every `get` command (`users`, `apps`, `functions`, `automations`,
  `requests`, ...) passes the API response through as-is, wrapped under the
  endpoint's own top-level key (`function`, `app`, `userView.user`, ...). An
  unqualified `--fields id` — the obvious first thing to try, and exactly what
  the unfiltered output shows — matched nothing at the root and printed `{}`
  while still exiting `0`, reporting success for a request that returned no
  data. Two changes close this: (1) a name that doesn't resolve from the root
  is now also searched for inside the wrapper, depth-insensitively, so
  `--fields id,displayName` on `functions get` finds `function.id`/
  `function.displayName` without the caller needing to know the wrapper key
  (the full path still works and is tried first; if the same name exists at
  more than one depth, the shallowest match wins, with same-depth ties broken
  by the alphabetically first full path, deterministically — the same
  tie-break rule already used for the snake_case/camelCase fallback above);
  (2) regardless of (1), a `--fields` spec matching **nothing at all** in the
  response — a genuine typo, or a field that truly doesn't exist — is now a
  usage error (exit `2`) instead of a silent `{}`. This is a zero-match check
  only, by design: a typo among several fields (`--fields id,dispalyName`)
  still exits `0`, silently dropping just the misspelled one, because
  `--fields`/`C1I_FIELDS` is a persistent, session-wide setting and one spec is
  routinely reused across differently-shaped responses — erroring on any
  unmatched name would make a session-wide `C1I_FIELDS` fail on every command
  whose response legitimately lacks one of the names. List/NDJSON output is
  unaffected: list rows are already flat maps the CLI builds itself, not a
  wrapped API passthrough, so the defect never applied there and no-match
  rows there still emit `{}` per row.
- **`mcp gateway` failures now classify to the standard exit-code taxonomy.**
  A gateway HTTP failure previously exited `1` for everything except a 401/403
  at the handshake step (which alone was mapped to `3`); a 404/429/5xx from
  `list-tools` or `call` — including any failure after a successful handshake,
  since the one-off classifier only ran on `Initialize` — was indistinguishable
  from a generic error. `*mcpgateway.HTTPError` now unwraps to a
  `*client.APIError`, so every gateway call threads through the same taxonomy
  every other API failure gets (401/403 → 3, 404 → 4, 429 → 5, 5xx → 6),
  without losing the response body from the error message. The one-off
  handshake-only classifier is removed as redundant.
- **A JSON-RPC-level `mcp gateway` failure now classifies too, not just an
  HTTP-level one.** The fix above covers a non-2xx HTTP response; a gateway
  answering 200 with a JSON-RPC `error` (initialize/tools/list/tools/call all
  go through the same JSON-RPC envelope) still exited `1` regardless of code —
  verified live: naming a nonexistent tool (`-32602`) or an unimplemented
  method (`-32601`) both exited `1`, and so did every upstream connector
  failure (an unreachable external MCP server, a vendor API error), which
  arrives as JSON-RPC code `0`. `-32602` now exits `2` (usage — the caller
  named a tool with bad arguments). `-32601`, the other protocol-level codes,
  and an upstream connector failure exit `8` — see the exit-`8` entry under
  Changed for that split. Any other JSON-RPC code still exits `1`, unchanged. Error
  messages are unchanged, only the exit code.

  No HTTP status is fabricated for these: the gateway answers 200, and
  `--error-format json` renders a status when one is present, so inventing one
  would put a false fact about the wire into machine-readable output.
- **`requests create grant`/`requests create revoke --user-id`'s "defaults to
  self if omitted" is now true for both.** Omitting it used to send no
  `identityUserId` at all, which the API rejects with a
  `500 {"code":2,"message":"user_id is required"}` — a documented default
  the flag never actually had, on both commands (they share the same wire
  contract and the same help text). Both now resolve the caller's own user id
  via the same introspect-based `currentUserID` lookup `requests list`'s
  default requester scope and `tasks list --assigned-to-me` already use,
  costing one extra `GET /api/v1/auth/introspect` call only when `--user-id`
  is omitted. `--dry-run` also resolves self before building its preview
  (authenticating before the dry-run check, like `tasks approve`/`deny`
  already do to resolve the task's policy step) so the previewed body
  includes `identityUserId` and matches what the real call actually sends —
  an earlier version of this fix left `--dry-run` previewing a body without
  it while the real call sent one; `README.md`'s dry-run section now lists
  both commands alongside `tasks approve`/`deny` as needing credentials when
  `--user-id` is omitted. An explicit `--user-id`
  still needs no extra call, dry-run or not. Verified live end to end: a
  grant created with `--user-id` omitted, then revoked with `--user-id`
  omitted, both populated `identityUserId` with the caller's own id.
- **`extractSSEResponse` now follows the SSE spec exactly.** Multiple `data:`
  lines within one SSE event are joined with `\n` (previously concatenated
  with no separator, which could corrupt a multi-line payload), and exactly
  one optional leading space after `data:` is stripped — previously
  `TrimSpace` also ate meaningful leading/trailing whitespace inside the
  payload. The response event is now selected by matching the request's
  JSON-RPC `id` first, falling back in order to an event carrying `result`/
  `error`, then the last event, then the raw body on a scan error — so a
  reply to a different in-flight request can no longer be mistaken for the
  caller's own.
- **`extractSSEResponse` no longer falls back to "the last event" when no
  event answers the request.** A follow-up review of the fix above found that
  if a stream carried no event matching the request's id
  and none carrying `result`/`error` — e.g. only a progress notification —
  the old third-tier fallback returned that notification's bytes as if they
  were the response. `decodeMessage` parses that fine (it has neither
  `result` nor `error`), so `mcp gateway call`/`list-tools` read "the server
  never answered" as a successful empty response. It now returns the raw
  body instead, the same as the existing scan-error path, so the failure
  surfaces as a visible decode error rather than a silent success. This is a
  latent bug, not one observed in the wild: C1's gateway has never been seen
  returning a multi-event SSE stream on a POST across three independent
  live-capture sessions, so this fallback tier was unreachable against C1
  today; it guarded a spec-permitted shape the client advertises support for
  but has not observed.
- **`extractSSEResponse`'s result/error scan no longer discards a response
  whose `id` isn't a plain non-null integer.** A follow-up review of the fix
  directly above found that its tier-2 scan (the event carrying
  `result`/`error`) was gated on the event's `id` decoding as a non-null
  `*int`, so an event with a string `id`, or with `id: null`, was skipped by
  tier 2 and fell through to the new raw-body return — turning a legitimate
  response into a confusing decode failure. Like the two items above this is latent
  against C1 today — `extractSSEResponse` only runs when the response is
  `text/event-stream`, and C1 has never been observed returning that on POST.
  It differs in how little it would take to fire: the others need a
  multi-event stream, whereas this needs only a single SSE-framed error
  response, because JSON-RPC 2.0 *requires* `id: null` specifically
  when the server couldn't determine the request's id — "If there was an
  error in detecting the id in the Request object (e.g. Parse error/Invalid
  Request), it MUST be Null" (jsonrpc.org/specification) — so any
  spec-compliant gateway that framed responses as SSE would hit this the
  first time it answered a malformed request with a `-32700`/`-32600` error,
  and have its actual error replaced by "parsing gateway response: invalid
  character...". The
  result/error scan no longer looks at `id` at all — presence of
  `result`/`error` is already sufficient to distinguish a response from a
  notification or a server-initiated request, both of which carry
  `method`/`params` and never `result`/`error`.
- **`mcp gateway call` no longer fails open when `isError` is present but not
  a JSON boolean.** A follow-up review of the change that added the `isError`
  check found that `toolResultIsError` decoded into a
  `bool` field, so a server sending `isError` as the string `"true"`, a
  number, an object, or an array made `json.Unmarshal` fail on the type
  mismatch — and the old code treated that decode failure as `isError: false`
  (success), narrowly reintroducing the exact "tool failure read as success"
  bug exit code `7` exists to catch. Any `isError` value other than `false`,
  `null`, or absent (all still success) or the literal `true` (still exit `7`)
  now also exits `7`, with a diagnostic that calls out the non-boolean value
  so it reads differently from a genuine `isError: true` failure. This is a
  deliberate, accepted tradeoff: MCP defines `isError` as a boolean, so any
  non-boolean value already means a non-conformant tool server, and a
  spec-violating falsy value like `isError: 0` will now also be reported as
  an error rather than special-cased back to success. This path requires a
  non-conformant server to trigger; it has not been observed against C1's own
  gateway.

- **`apps set-owners --wait` no longer claims to be "still waiting" before it
  has waited.** The poll loop queries once immediately, with no delay, so a
  first-pass miss printed "Still waiting for owners to provision … (0s
  elapsed)…" — which read as though time had already passed. The first check
  is now silent; subsequent polls report progress as before. The success line,
  the timeout error, the poll interval, and the default timeout are unchanged.
- **`mcp gateway` HTTP errors now name the failing RPC.** MCP is a
  single-endpoint protocol, so every gateway failure reported the same
  `POST` and path and gave no way to tell an `initialize` failure from a
  `tools/list` or `tools/call` one — including in `--error-format json`. The
  error now carries and renders the JSON-RPC method, and still includes the
  response body. Exit-code classification is unchanged and is now pinned by a
  test across 401/403/404/429/500/503.
- **`mcp gateway list-tools` can no longer paginate forever.** The
  `tools/list` cursor loop had no termination guard, so a gateway that kept
  returning the same cursor — or an ever-changing one — would spin
  indefinitely. It now fails with a clear diagnostic when a cursor repeats, and
  has an absolute page-count backstop. Both paths return an error rather than a
  partial list: silently truncating the tool list is the exact failure this
  client already had fixed once, and returning "what we got" would reintroduce
  it in a new form.
- **`c1i api` invalid-invocation guards now exit `2` (usage), not `1`.**
  **Breaking change for anything branching on exit codes:** `--limit`/
  `--list-key` without `--paginate`, `--body`/`--body-file` given together, an
  unsupported `--method`, a body on `--method GET`, a body on `--method
  DELETE` without `--allow-delete-body`, and a malformed `key=value` in
  `--query`/`--header` all used to return a bare, unclassified error (exit
  `1`). They're now wrapped in the same `usageError` that every other bad
  flag/argument combination already uses, so `c1i api` joins the rest of the
  CLI in exiting `2` for a malformed invocation. This includes the
  `--method DELETE --body` refusal added in the previous entry above, which
  shipped exiting `1` and now exits `2` — a caller that special-cased `1` for
  that specific refusal needs to check `2` instead. No error message text
  changed, only the exit code.
- **A stray positional argument is no longer silently ignored on ~38
  commands.** Any runnable command that left `Args` unset fell back to
  cobra's `ArbitraryArgs`, so e.g. `c1i mcp servers list somejunk` ran
  normally, ignored `somejunk`, and exited `0` — reading as success for a
  probably-typo'd invocation. Every runnable command whose `Args` was nil now
  defaults to `cobra.NoArgs` at startup, so a stray argument is rejected with
  a usage error (exit `2`) instead. This never touches a command that already
  declares its own `Args` (e.g. a `get <id>`-style command with
  `cobra.ExactArgs(1)`), and command groups (`mcp`, `mcp servers`, ...) keep
  their existing "print help on no args / error on an unknown subcommand"
  behavior unchanged.
- **`docs guide test-mcp-gateway` no longer claims every gateway-exposed tool
  needs a toolset grant.** Disproven live: a caller with no grant on the C1
  app's `search_tools` entitlement still gets `search_tools` back from `mcp
  gateway list-tools`. The guide now distinguishes tools discovered from a
  registered connector (HOSTED/EXTERNAL/tunneled MCP server) — which do
  require holding the entitlement of a binding toolset — from C1's own
  built-in `c1_*` tools, whose gateway exposure tracks the caller's
  underlying role and permissions instead, unaffected by any toolset grant.
  This is about exposure/listing only; whether `c1_*` tool *execution* is
  similarly unconfined was not re-verified. `docs guide
  assign-toolset-everyone` gets a matching one-sentence caveat, since the
  toolset mechanism it walks through has no effect on `c1_*` tools either.
- **`cmd/agents.md` no longer implies a task's `outcome` only appears once
  it closes.** Disproven live: two tasks were `TASK_STATE_OPEN` and already
  carried `GRANT_OUTCOME_ERROR` (provisioning failed mid-flow). The
  omission mechanism was right — `outcome` is dropped while unspecified —
  but the semantic inference wasn't: an open task can already carry a real
  outcome, so the doc now says to key off `state`, not the presence of
  `outcome`, to tell whether a task is still pending.
- **`cmd/agents.md`'s tenant/auth section named the wrong command for
  confirming the tenant.** It pointed at `c1i auth whoami` for "who and
  where you are," but `whoami`'s output (`userId`, `principleId`, `email`,
  `displayName`, `counts`) never includes the tenant URL — `c1i auth
  status` is what prints it (`Authenticated to <base-url>`). The doc now
  names `auth status` for the tenant and `auth whoami` for the identity,
  which matters here specifically because the surrounding section is about
  not silently targeting the wrong tenant.

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

[Unreleased]: https://github.com/ConductorOne/c1i/compare/v0.4.1...HEAD
[0.4.1]: https://github.com/ConductorOne/c1i/releases/tag/v0.4.1
[0.4.0]: https://github.com/ConductorOne/c1i/releases/tag/v0.4.0
[0.3.0]: https://github.com/ConductorOne/c1i/releases/tag/v0.3.0
[0.2.1]: https://github.com/ConductorOne/c1i/releases/tag/v0.2.1
[0.2.0]: https://github.com/ConductorOne/c1i/releases/tag/v0.2.0
