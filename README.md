# c1i

**C1 Interface** — and it looks like `cli`. Get it?

A command-line interface for the [C1.ai](https://www.c1.ai/) API built
for AI agents and scripts. Machine-readable output (NDJSON/JSON), explicit exit
codes, built-in API docs, and auto-pagination make it reliable to automate.
For a human-friendly CLI, see [cone](https://github.com/ConductorOne/cone).

## Installation

Download signed macOS, Linux, and Windows releases with checksums, provenance, and SBOM attestations from the [official distribution center](https://dist.conductorone.com/ConductorOne/c1i).

```sh
# Homebrew
brew install conductorone/baton/c1i

# Go
go install github.com/ConductorOne/c1i@latest

# Container (image tags omit the leading "v" -- e.g. 0.9.0, not v0.9.0)
docker pull public.ecr.aws/conductorone/c1i:<version>
```

A standalone binary updates itself with `c1i upgrade`; see [Upgrading](#upgrading).

## Start Here

Use this workflow for agent-driven and scripted operations:

1. Run `c1i docs agents` before operating on a tenant. It explains tenant
   selection, output and exit-code contracts, mutation safety, and raw API use;
   it needs no authentication.
2. Pass `--url https://<tenant>` on every C1 command, then authenticate and run
   `c1i auth whoami` before doing anything else.
3. Run every intended C1 REST write with `--dry-run` first. Gateway tool calls
   reject it and are always live.
4. Prefer a first-class command. Use `c1i api` only when no command covers the
   public REST endpoint.

## Quick Start

```sh
# Pass --url explicitly on every C1 call to avoid falling back to another tenant.
c1i auth login --url https://mycompany.conductor.one

# Verify the authenticated identity and tenant.
c1i auth whoami --url https://mycompany.conductor.one

# List users.
c1i users list --url https://mycompany.conductor.one

# Explore the API — no credentials needed
c1i docs search "access reviews"
c1i docs endpoints --filter task
```

## Command Reference

### Users

```sh
c1i users list [--query <text>] [--email <email>] [--status enabled|disabled|deleted] [--origin directory|local|system|all]... [--page-size N] [--page-token TOKEN] [--limit N]
c1i users get <user-id>
```

`users list` reads `POST /api/v1/search/users`, which with no `--origin` returns only directory-origin users: local-origin users (service principals among them) and system users are excluded. `--origin all` lists every user; repeat `--origin` with `directory`, `local`, `system` or a `USER_ORIGIN_*` value (as a row's `origin` prints) to pick origins (any other value is a usage error, exit `2`). Each row carries `origin` (e.g. `USER_ORIGIN_LOCAL`) when the server reports one.

### Apps

```sh
c1i apps list [--page-size N] [--page-token TOKEN] [--limit N]
c1i apps get <app-id>
c1i apps create --display-name <name> [--description <text>] [--grant-policy-id <id>] [--revoke-policy-id <id>] [--certify-policy-id <id>] [--identity-matching strict|display-name|custom]
c1i apps update <app-id> [--display-name <name>] [--description <text>] [--grant-policy-id <id>] [--revoke-policy-id <id>] [--certify-policy-id <id>] [--unbind-policy grant|revoke|certify]... [--identity-matching strict|display-name|custom] [--app-user-mapper-file <path|->]
c1i apps owners <app-id> [--page-size N] [--page-token TOKEN] [--limit N]
c1i apps add-owner <app-id> --user-id <id>
c1i apps remove-owner <app-id> --user-id <id>
c1i apps set-owners <app-id> --user-id <id> [--user-id <id> ...] [--wait] [--wait-timeout 4m]
c1i apps delete <app-id>
```

`user_count` is the platform's stored count, not a live tally: on some apps it differs from the number of `accounts list --app-id <app-id>` rows. To size a change, count those rows, not `user_count`.

`apps create` needs only `--display-name`; it makes a plain, unmanaged
container app — the zero state for "make an app, then register MCP servers
under it". The caller is auto-assigned as an owner, showing up in `apps owners`
after the usual provisioning lag. The new app prints as pretty JSON, unwrapped
like `apps get`, so read its id with `jq -er .id` (`-e` fails on a `--dry-run`
preview, where `jq -r` exits 0 with `null`). An explicit `--fields` on a
mutation is a usage error (exit `2`).

`apps update` sends only the fields whose flags you pass, and prints the
updated app the same way. `--grant-policy-id`, `--revoke-policy-id` and
`--certify-policy-id` bind policies, on `apps create` too (omitted at create,
the server assigns its default grant and revoke policies and no certify
policy). Before sending, c1i fetches each policy and refuses (exit `2`,
nothing sent) an id that doesn't exist, is deleted, or isn't of the flag's
type; `--dry-run` skips this check. An empty policy id is a usage error, not
an unbind: unbind with `--unbind-policy grant|revoke|certify` (repeatable),
which can't be combined with setting the same kind.
`--identity-matching` takes `strict`, `display-name`, `custom`, or the full
`APP_USER_IDENTITY_MATCHING_*` name; any other value is a usage error (exit
`2`). `--app-user-mapper-file` replaces the whole mapper with a JSON object
such as
`{"mappingCases":[{"userKeyCel":"subject.email","appUserKeyCel":"account.email"}]}`
(`subject` is the C1.ai user, `account` the app user); the server rejects bad
CEL with a 400 (exit `2`). `mappingCases` must be non-empty: the server
accepts an empty mapper with exit `0` but keeps the old one, and the API has
no way to remove a mapper.

`apps delete` is a soft delete: the app is marked with `deletedAt` rather than
erased, which is why `apps list` rows carry a `deleted_at` field. It prints the
API's response, `{}`; exit `0` is the success signal. Both commands honor
`--dry-run`.

`apps set-owners` prints the API's JSON response and returns as soon as the
`PUT` is accepted. Pass `--wait` to block and poll `GET .../ownerids` until the
owner set equals the requested ids, removals included, or `--wait-timeout`
(default `4m`) elapses. Its progress lines go to stderr, so stdout stays the
write's JSON response. A wait failure reports `"applied": true` under
`--error-format json`. A timeout exits `1` even though the write itself was
accepted — provisioning can simply still be in flight, so re-check with
`apps owners` rather than re-issuing the write. With `--dry-run` the preview
still only covers the `PUT`; `--wait` never polls.

The API accepts an `apps set-owners` user id that matches no user at exit `0`
and stores it (`GET .../ownerids` lists it, `apps owners` does not), so a set
naming only unknown ids leaves no real owners; `set-owners` therefore looks
up each distinct `--user-id` first and exits `2` naming an unknown one,
sending nothing (`--dry-run` skips the lookups). A repeated `--user-id` is
sent once.

`apps owners` is the read that reflects what `apps add-owner`,
`apps remove-owner` and `apps set-owners` write. Owner changes are applied
asynchronously and can take several minutes to appear. It returns zero rows
at exit 0 for a well-formed but nonexistent app id, so an empty result can
also mean "wrong id". `apps add-owner` and `apps remove-owner` change one
owner at a time (a single `--user-id`; use `set-owners` for many); two
`add-owner` calls issued at once were both observed to land.
`apps set-owners` replaces the whole list with exactly the ids you pass, so an
owner added between your read and your write is silently removed.

### Accounts

```sh
c1i accounts list --app-id <id> [--status enabled|disabled|deleted] [--type user|service-account|system-account] [--unmapped-only] [--query <text>] [--page-size N] [--page-token TOKEN] [--limit N]

c1i accounts set-owner <app-user-id> --app-id <id> --user-id <id>
```

`accounts set-owner` prints the app user as pretty JSON, unwrapped like the
reads: `jq -er .id` is the account and `.identityUserId` its owner.

### Entitlements

```sh
c1i entitlements list [--app-id <id>] [--query <text>] [--page-size N] [--page-token TOKEN] [--limit N]
c1i entitlements get <entitlement-id> --app-id <id>
c1i entitlements delete <entitlement-id> --app-id <id>
c1i entitlements owners <entitlement-id> --app-id <id> [--page-size N] [--page-token TOKEN] [--limit N]
c1i entitlements add-owner <entitlement-id> --app-id <id> --user-id <id> [--wait] [--wait-timeout 4m]
c1i entitlements remove-owner <entitlement-id> --app-id <id> --user-id <id>
c1i entitlements set-owners <entitlement-id> --app-id <id> --user-id <id> [--user-id <id> ...] [--wait] [--wait-timeout 4m]

# Directional entitlement-to-entitlement proxy binding
c1i entitlements proxy-bindings get \
  --source-app-id <id> --source-entitlement-id <id> \
  --destination-app-id <id> --destination-entitlement-id <id>
c1i entitlements proxy-bindings create \
  --source-app-id <id> --source-entitlement-id <id> \
  --destination-app-id <id> --destination-entitlement-id <id>
c1i entitlements proxy-bindings delete \
  --source-app-id <id> --source-entitlement-id <id> \
  --destination-app-id <id> --destination-entitlement-id <id>

# Create one on a manually-managed app, with its resource type and resource
c1i entitlements create --app-id <id> --display-name "Payroll admin" \
  [--description <text>] [--slug member] [--alias payroll_admin] [--owner-id <user-id>] \
  [--duration-grant 3600s] [--resource-type CUSTOM] \
  [--resource-type-display-name "Payroll role"] [--resource-display-name "Payroll admins"] \
  [--grant-policy-id <id>] [--revoke-policy-id <id>] [--certify-policy-id <id>] \
  [--emergency-grant-policy-id <id>]

# Reuse an existing resource type (or an existing resource) instead of creating one
c1i entitlements create --app-id <id> --display-name "Payroll viewer" --resource-type-id <id>
c1i entitlements create --app-id <id> --display-name "Payroll viewer (RO)" \
  --resource-type-id <id> --resource-id <id>

# Update name, description, policies, or max grant duration (only the flags passed)
c1i entitlements update <entitlement-id> --app-id <id> \
  [--display-name <name>] [--description <text>] \
  [--grant-policy-id <id>] [--revoke-policy-id <id>] [--certify-policy-id <id>] \
  [--emergency-grant-policy-id <id>] [--emergency-grant-enabled[=false]] \
  [--unbind-policy grant|revoke|certify|emergency-grant] \
  [--duration-grant 8h | --duration-unset]
```

`entitlements delete` is a soft delete (`entitlements get` still returns the
entitlement, with `deletedAt` set) and prints `{}`. `apps delete` does not
delete an app's entitlements, so remove them with this first.

`entitlements update` builds the update mask from exactly the flags you pass.
Each policy id is checked before anything is sent: it must exist, not be
deleted, and be of the slot's type, or the command exits `2`.
`--emergency-grant-policy-id` takes a grant policy; the server enforces that
only when the same call enables emergency grants (`emergency policy id must
reference a grant policy`) and otherwise stores any id. An empty policy id is
exit `2`; unbind with the repeatable `--unbind-policy`.
`--emergency-grant-enabled` needs `--emergency-grant-policy-id` in the same
call even when one is already stored (the server's `emergency grant policy id
must be set if emergency grant is enabled`), so it is refused at exit `2`
without one, or alongside `--unbind-policy emergency-grant`. Unbinding the
emergency policy while emergency grants stay enabled is accepted by the server.
`--duration-grant` takes a whole-number Go duration (`8h`, `1h30m`, `3600s`;
no sign, no decimals) and is sent in seconds; `-1h` or `1.5h` exits `2`, and
`0s` is sent and was stored by the server without complaint. `--duration-unset`
removes the limit, and the two are mutually exclusive. A blank `--display-name`
is refused:
the server would accept "" and restore the entitlement's original name.
`entitlements create` checks its policy flags the same way before its first
`POST`, so a bad policy creates nothing.

An entitlement points at an app resource, which lives under an app resource
type, so `entitlements create` is up to three `POST`s: the resource type, the
resource, then the entitlement. `--resource-type-id` and `--resource-id` skip
whichever of the first two steps you already have — one resource type can carry
many resources, and one resource many entitlements. The server requires both
ids on the entitlement even though the OpenAPI schema marks only `displayName`
required, so `--resource-id` without `--resource-type-id` is rejected at exit
`2` before anything is sent.

`--resource-type` is `ROLE`, `GROUP`, `LICENSE`, `PROJECT`, `CATALOG`,
`CUSTOM`, `VAULT` or `PROFILE_TYPE` (case-insensitive here, uppercase on the
wire; defaults to `CUSTOM`) and describes the resource type this command creates, so passing it
together with `--resource-type-id` is a usage error rather than a silently
ignored flag. Only `CUSTOM` can repeat on one app: a second resource type of
any other kind fails with a 500 (exit `6`, though retrying never helps) saying
`app resource type already exists`, so reuse the existing one with
`--resource-type-id` and drop both `--resource-type` and
`--resource-type-display-name` — either one alongside the id is a usage error.
Reusing a resource with `--resource-id` likewise means you drop
`--resource-display-name`. `--owner-id` is repeatable and goes inline in the
create request, so no follow-up call is needed; an empty one is a usage error
rather than an owner quietly dropped, and each id is checked before the first
create (an unknown user exits `2` and creates nothing; the check reads the user,
so a credential without read access exits `3`, and `--dry-run` skips it). `--duration-grant` takes a protobuf
(`3600s`) or Go (`8h`) whole-number duration (no sign, no decimals), sent in
seconds; anything else, or an empty one, is a usage error. Omit it for standing access.

`--dry-run` previews all three requests in `requests[]`; where an id only
exists after a real preceding step, `NEW_APP_RESOURCE_TYPE_ID` and
`NEW_APP_RESOURCE_ID` appear literally in the `url` and `body`, and a stderr
notice explains them. There is no rollback: if a later step fails, the objects
the earlier ones created still exist, and the error names them along with the
flags that reuse them and the create-only flags the retry has to drop. The
created entitlement is printed as pretty JSON, unwrapped like
`entitlements get` (`jq -er .id`; an explicit `--fields` is a usage error); it
echoes `appResourceTypeId`/`appResourceId` and expands both objects, so every
id the command touched is in that one payload. Under `--error-format json` a
failed run reports `applied` (`true` once any step was written) and a `steps`
array naming each attempted request.

`entitlements owners` lists an entitlement's owners (same row shape as
`apps owners`) and is the read that reflects `entitlements add-owner`,
`remove-owner` and `set-owners`. Owner changes are applied asynchronously
and can take several minutes to appear; pass --wait to block until they do.
`--wait` polls `GET .../ownerids` until the change lands, or `--wait-timeout`
(default `4m`) elapses: on `add-owner` until the owner appears, on
`set-owners` until the owner set equals the requested ids, removals
included. A timeout exits `1`, though the write was accepted. Its progress
lines go to stderr, so stdout stays the write's JSON response.

`set-owners` replaces the whole list and needs at least one `--user-id`,
since the API takes an empty list as "remove every owner"; `remove-owner`
can still remove the last owner. The API accepts a `set-owners` user id that
matches no user at exit `0` and drops it, so a set naming only unknown ids
leaves no owners; `set-owners` therefore looks up each distinct `--user-id`
first and exits `2` naming an unknown one, sending nothing (`--dry-run` skips
the lookups). A repeated `--user-id` is sent once, since the API answers a
duplicate with a 500. An unknown user, entitlement or app exits `4` on
`add-owner`.
`remove-owner` exits `4` on a user who was never an owner as well as on an
unknown one, so `4` cannot tell those apart; a user who was an owner and is
already removed exits `0`. `owners` returns zero rows at exit `0` for a
nonexistent entitlement or app.

`entitlements proxy-bindings` manages directional entitlement-to-entitlement
visibility and tracking links. Source and destination are each identified by
their app and entitlement IDs; entitlement IDs are app-scoped. Creating a
binding does not grant access or configure delegated provisioning. Use
`c1i docs guide delegate-entitlement-provisioning` for the separate ordered
workflow that configures delegation on the destination entitlement. The public
REST API has no proxy-binding list endpoint; the C1 Console's binding browser
uses an internal gRPC-web search service that is outside the public API
contract.

### Grants ("who has access")

```sh
c1i grants list --app-id <id> --entitlement-id <id>   # who holds an entitlement
c1i grants list --user-id <id>                         # what a C1 identity has, across apps
c1i grants list --app-user-id <id>                     # what an app account holds
c1i grants list --app-id <id>                          # every grant in an app

# After a grant, wait for the set to stop changing rather than polling by hand
c1i grants list --app-id <id> --entitlement-id <id> --wait --wait-min 1
```

Grants are the bindings between accounts/users and entitlements. At least one
filter is required. Each NDJSON row includes the entitlement, the account
(`app_user_*`) and its `identity_user_id`, timestamps (`created_at`,
`deprovision_at`), and `grant_source_count` — `0` for a direct grant, or the
number of groups/roles the access is inherited through.

Grant provisioning is asynchronous, so a read taken moments after a grant or
revoke can catch the set mid-change. `--wait` re-reads every page every 5s and
prints nothing until the same grants come back `--wait-stable` times running
(default `3`, minimum `2` -- one read cannot show that anything held steady).
Progress goes to stderr, so stdout stays pure NDJSON. The 5s interval is fixed
and is not a flag; `--wait-stable` must fit inside `--wait-timeout` (default
`4m`), and a combination that cannot fit is rejected as a usage error rather
than left to time out. A timeout exits `1` and prints no rows.

**An empty result is stable.** A filter matching nothing settles at the first
opportunity -- about 10s at the defaults -- and exits `0` with zero rows.
Waiting on a grant you just made, that reads as "it did not happen" when the
truth is "not yet". Pass `--wait-min 1` (or the count you expect) to hold out
for that many grants and time out instead; exit `1` then means "did not
converge in time", not "absent". The default of `0` is deliberate:
empty-and-stable is the correct answer when you are waiting for a revoke --
and in that direction there is no flag that helps, since `--wait-min` is a
floor and cannot express a ceiling. `--wait` settles on whatever is steady, so
exit `0` still listing the row means "not yet, re-run", not "the revoke
failed".

`--wait` settles on the whole matching set, fetching every page on every poll
regardless of `--limit`; `--limit` only truncates what is printed. Filter
narrowly. `--wait` and `--page-token` are mutually exclusive.

A grant outlives the entitlement or account it points at, so rows also carry
`entitlement_deleted_at` and `app_user_deleted_at`: `jq
'select(.entitlement_deleted_at)'` finds grants whose backing object is gone.
Absent timestamps are `null`, never `""`.

### Tasks

```sh
c1i tasks list [--state open|closed] [--query <text>] [--assigned-to-me] [--page-size N] [--page-token TOKEN] [--limit N]
c1i tasks get <task-id>
c1i tasks approve <task-id> [--policy-step-id <id>] [--comment <text>]
c1i tasks deny <task-id> [--policy-step-id <id>] [--comment <text>]
c1i tasks comment <task-id> --comment <text>
c1i tasks close <task-id> [--comment <text>]
c1i tasks reassign <task-id> --to-user-id <id> [--to-user-id <id> ...] [--policy-step-id <id>] [--comment <text>]
c1i tasks restart <task-id> [--policy-step-id <id>] [--comment <text>]
c1i tasks reset <task-id> [--comment <text>]
c1i tasks skip-step <task-id> [--policy-step-id <id>] [--comment <text>]
c1i tasks process <task-id>
c1i tasks update-grant-duration <task-id> --duration <duration>
```

`restart`, `reset` and `skip-step` each rotate the task's current policy step,
so a `--policy-step-id` captured before one of them goes stale — the server
answers `this action is no longer available: the request has advanced to a new
approval step`. Omit the flag to act on whatever step is current.

Which actions a task accepts depends on its state; the server refuses the rest
with `action not permitted`. Read the task's own list with
`c1i api --path /api/v1/tasks/<task-id> --fields actions`.

`restart` re-runs the current approval step; `reset` restarts the whole policy.
Either can reopen a closed task, but whether it does depends on how the task
closed: `reset` reopened CANCELLED, GRANTED and REVOKED closes but not a DENIED
one — exit `0`, the task stays CLOSED with outcome DENIED. `restart` reopened a
GRANTED close but answered `task is closed` (exit `2`) on CANCELLED and DENIED
ones (REVOKED untested). Neither is a general undo: re-read with `c1i tasks get`
instead of trusting either exit. `process` changes nothing observable on a
healthy task — it is intended for one that has stalled, which was not
reproduced here.
`update-grant-duration` takes a whole-number Go (`1h`) or
protobuf (`3600s`) duration (no sign, no decimals), sent in seconds; `-1h` or
`1.5h` exits `2`, and `0s` is sent and was stored by the server without
complaint. It only applies before the task reaches provisioning, after which the server answers `cannot update grant duration for
a ticket in a provision step`; the value lands at `.type.grant.grantDuration`
(there is no top-level `.grantDuration`).

`escalate`, `update-request-data` and `approve-with-step-up` are not wrapped;
reach them through `c1i api`.

`approve`/`deny`/`reassign`/`skip-step` target a specific policy step, fetched
when `--policy-step-id` is omitted. `approve`, `skip-step` and `reassign`
require a resolvable step: a task with no current step exits `2` and tells you
to re-read it with `tasks get` (`--policy-step-id` cannot supply one), while
`deny` and `restart` proceed without the field when the task has no current
step. Because each of them reads the task first, an unknown task id exits `4`
before any action is sent.

`close` cancels a task without recording an approval decision, and takes no
step. Closing an already-closed task is rejected by the API with `task is
closed` (exit 2).

`reassign` sets the step's approvers to the users named by `--to-user-id`;
repeat the flag to assign several. Before sending it looks each id up: an
unknown user exits `2` and nothing is sent, because the API would store the id
as the step's approver. `--dry-run` skips that lookup, so a preview does not
prove the ids exist. A repeated id is sent once; recover a step already given a
bad approver by re-running `reassign` with valid ids (a system owner can also
`reset`/`restart` where the task offers them).

Every task action prints the task unwrapped like `tasks get` (`jq -er .id`),
plus `ticketActionId` (`taskActionId` on `close`) where the API returns one. The
endpoints return the task as it was *before* the action, so `.state` is a
snapshot: closing an open task prints `TASK_STATE_OPEN`. Re-read with `tasks
get` before acting on it. A policy step c1i looked up for you is reported on
stderr (`Resolved policy step: <id>`), never in the document.

### Connectors

```sh
c1i connectors list --app-id <id> [--page-size N] [--page-token TOKEN] [--limit N]
```

### Functions

```sh
c1i functions list [--published-only | --draft-only] [--page-size N] [--page-token TOKEN] [--limit N]
c1i functions get <function-id>
c1i functions source <function-id> [--commit <id>] [--out-dir <path>]
c1i functions commits <function-id> [--page-size N] [--page-token TOKEN] [--limit N]
c1i functions usage <function-id> [--page-size N] [--page-token TOKEN] [--limit N]
```

`functions source` auto-resolves the function's published commit (falling back to its head/latest draft) and base64-decodes the source files. Without `--out-dir`, each file is printed to stdout with a `// ===== <name> =====` delimiter; with `--out-dir`, files are written to disk. Fetched source is developer-authored code that commonly inlines credentials, so files are written `0600` and the directory is created — or, if it already exists, tightened, never widened — to at most `0700` (owner-only; group access to the directory would buy nothing since the files inside are already unreadable to group/other, and a filename alone can be informative). Any setuid/setgid/sticky bit on the directory is stripped outright, since none of them mean anything once there is no group or other access left. A tightened pre-existing directory prints a warning to stderr naming its old mode. `functions usage` scans every automation and emits one row per step that calls the given function — useful before deleting a draft.

`functions list` and `functions commits` stop if the server's pagination
cursor stops advancing — a repeated `nextPageToken`, or two or more pages in
a row (at least 25 rows) holding only rows already seen, which these routes can do on large result
sets. The command keeps the rows already printed, warns on stderr that
results are INCOMPLETE and names the route, and exits `6`. `functions usage`
reads `POST /api/v1/search/automations`, which pages correctly, and keeps the
same guard as a backstop.

### Automations

```sh
c1i automations list [--enabled-only] [--calls-function <fid>] [--page-size N] [--page-token TOKEN] [--limit N]
c1i automations get <automation-id>
c1i automations update <automation-id> [--display-name NAME] [--description TEXT] [--enabled[=false]]
c1i automations update <automation-id> --body-file automation.json --update-mask triggers
c1i automations delete <automation-id>
c1i automations executions list [--state done|error|pending|...] [--template-id <tid>] [--page-size N] [--page-token TOKEN] [--limit N]
```

Each `automations list` row includes `function_ids` (every distinct function the automation invokes), so `--calls-function` can answer "which automations call function X?". `executions list --state` accepts the short forms (`done`, `error`, `pending`, ...) or the full `AUTOMATION_EXECUTION_STATE_*` enum; state and template filtering are applied client-side, so pair a narrow filter with `--limit` to bound the work.

`automations update` sends only the fields you pass (an explicit `--enabled=false` disables; a blank `--display-name` is refused). For triggers, steps, draft or circuit-breaker fields, pass an Automation object with `--body-file` and list the camelCase fields to change in `--update-mask`. Masking `triggers` replaces the published triggers and increments `currentVersion`; `draftTriggers` and `isDraft` are left as they were. `automations delete` is a soft delete: the automation leaves `automations list`, but `automations get` still returns it. `automations update` prints the automation unwrapped (`jq -er .id`), with `webhookCapabilityUrl` and `webhookHmacSecret` as top-level siblings; `automations delete` prints `{}`.

`automations list` and `automations executions list` read
`POST /api/v1/search/automations` and `POST /api/v1/search/automation_executions`
(the `GET` routes stop paging on large result sets) and keep the stuck-cursor
guard described under Functions above as a backstop.

### Policies

```sh
c1i policies list [--page-size N] [--page-token TOKEN] [--limit N]
c1i policies get <policy-id>
c1i policies search [--query <text>] [--display-name <name>] [--policy-type grant|revoke|certify ...] [--include-deleted] [--exclude-policy-id <id> ...] [--page-size N] [--page-token TOKEN] [--limit N]
c1i policies create --display-name <name> --policy-type grant|revoke|certify [--description <text>] [--steps-file <file|-> ] [--rules-file <file|-> ] [--allow-deny-all]
c1i policies create --body-file <file|->
c1i policies update <policy-id> [--display-name <name>] [--description <text>] [--policy-type ...] [--steps-file <file|-> ] [--rules-file <file|-> ] [--allow-deny-all]
c1i policies update <policy-id> --body-file <file|-> --update-mask <paths>
c1i policies delete <policy-id>
c1i policies validate-cel <condition>
```

A policy describes how C1 processes a task: who approves it (an ordered list
of `policySteps`, each a oneof of approval/provision/accept/reject/wait/form —
the schema also declares `action`, which the server rejects as an unsupported
step type — and an approval step's approver is itself a oneof of ten arms:
users, manager, group, appOwners, self, entitlementOwners, expression, webhook,
resourceOwners, agent), and how `rules[]` route a task to one of several
step sequences by CEL condition. That structure is too deeply nested for a
flag surface, so `create`/`update` take it from a JSON file (or `-` for
stdin) via `--steps-file`/`--rules-file`/`--body-file` — the same pattern
`mcp servers register` uses for its auth config. Only one of the three can
read stdin (`-`); two exit 2 before reading.

**Client-side guards** (`create` and `update`, before any request is sent,
exit code 2) exist because several C1 policy API defects are either silent
or return an opaque `HTTP 500` instead of a `400`:

- Empty/missing steps for a policy's baseline entry are refused —
  `POST /api/v1/policies` with no `policySteps` succeeds and silently
  returns a deny-everything policy (a single `{"reject":{}}` step), with no
  validation error. Pass `--allow-deny-all` if that's genuinely what you
  want (an explicit `steps:[]` is refused regardless — the server 500s on
  that, not a safe default).
- `--policy-type` unspecified, an empty `rules[].condition` (needs the
  literal `"true"` for a baseline/catch-all rule), a `provision` step,
  `fallback`/`fallbackUserIds` on an approver arm that doesn't support them
  (only `users`, `appOwners`, `webhook`, and `agent` lack it — the other six
  arms each support their own `fallback`/`fallbackUserIds`/
  `fallbackGroupIds`/`isGroupFallbackEnabled`), and `fallback:true` with
  nothing to fall back to (a bare server error that surfaces as `HTTP 500`).

`update` sends the API's required `{"policy": {...}, "updateMask": "..."}`
wrapper for you — a flat body 400s. The `--steps-file`/`--rules-file`
convenience flags derive the update mask from what you pass; `--body-file`
requires an explicit `--update-mask` (spaces around its comma-separated paths
are trimmed; an empty path, or `--update-mask` without `--body-file`, exits 2).

`create`/`update` print the policy unwrapped like `get` (`jq -er .id`); `delete`
prints `{}`. A `--steps-file` or `--body-file` update that states no
`policyType` looks the policy's current type up first (nothing is sent if that
fails); `--dry-run` skips the lookup, so pass `--policy-type` to preview one.

`validate-cel` checks a CEL condition without creating or updating anything.
Its root variable is `subject` (not `user`); this validates the
`rules[].condition` environment specifically, which is NOT the same
environment `ExpressionApproval.expressions` run in (see the command's
`--help`). An invalid condition prints its compile markers and exits 2, so
`c1i policies validate-cel '<cond>' && ...` only continues on a condition
that compiles.

A soft-deleted policy still returns from a direct `get` (with `deletedAt`
populated) but disappears from `list` and the default `search`; only
`search --include-deleted` finds it there. List and search rows carry
`deleted_at` so deleted rows are distinguishable without a second call — it
is `null` on a live policy, so `jq 'select(.deleted_at)'` selects only the
deleted ones.

`list`/`search` rows also carry `step_kinds` (the policy's baseline sequence in
run order, e.g. `["accept"]`) and `baseline_policy_id` — the portable way to
identify a tenant's auto-approval grant policy, since display names vary
(`"Auto-approval"` vs `"Auto approval"`). Match on the shape, not the name —
pipe `c1i policies list` into
`jq -c 'select(.policy_type=="POLICY_TYPE_GRANT" and .system_builtin and .step_kinds==["accept"] and .baseline_policy_id==null)'`.
A tenant can hold more than one such policy, so check what comes back rather
than taking the first row (`rule_count > 0` means conditional rules route to
alternative sequences). A non-null `baseline_policy_id` means the policy defers
its baseline to another — `step_kinds` is then `[]` for a healthy policy.
`policies get` is a verbatim API passthrough and exposes neither derived key;
use `list`/`search` for them.

### MCP

Drive the MCP admin surface (servers, tools, toolsets, and bindings). Most commands take `--app-id`; `mcp servers` commands take the server's `<connector-id>` positionally, while tool/toolset/binding commands scope to a server with `--connector-id`.

```sh
# Servers (register, configure, and inspect MCP servers)
c1i mcp servers list               --app-id <id> [--page-size N] [--limit N]
c1i mcp servers get                <connector-id> --app-id <id>
c1i mcp servers search             --app-id <id> [--query <text>] [--tool-state approved|pending|disabled|removed] [--include-last-called-at] [--limit N]
c1i mcp servers register           --app-id <id> --type hosted   --display-name <name> --catalog-id <cid> [--source-app-id <id>] [--auth ... ] [--config-field k=v ...] [--user-id <id> ...]
c1i mcp servers register           --app-id <id> --type external --display-name <name> --server-url <url> [--transport streamable-http|sse] [--auth ...] [--user-id <id> ...]
c1i mcp servers update             <connector-id> --app-id <id> [--display-name <name>] [--description <text>] [--data-sensitivity ...] [--tool-prefix <p>] [--require-tool-approval]
c1i mcp servers update-credentials <connector-id> --app-id <id> --type hosted|external [--auth ...] [--update-mask <paths>]
c1i mcp servers delete             <connector-id> --app-id <id>
c1i mcp servers resync-tools       <connector-id> --app-id <id>   # EXTERNAL only; 400 on HOSTED
c1i mcp servers test-connection    (--server-url <url> [--transport ...] [--auth ...] | <connector-id> --app-id <id>)   # EXTERNAL only; 400 on HOSTED; exits 8 when unreachable (JSON still printed)
c1i mcp servers discover-oidc      --issuer-url <url>
c1i mcp servers catalog list       [--query <text>] [--page-size N] [--limit N]
c1i mcp servers catalog get        <catalog-id>
c1i mcp servers connections list   [--page-size N] [--limit N]

# Tools
c1i mcp tools list    --app-id <id> --connector-id <id> [--page-size N] [--page-token TOKEN] [--limit N]
c1i mcp tools get     <tool-id> --app-id <id> --connector-id <id>
c1i mcp tools search  --app-id <id> --connector-id <id> [--query <text>] [--state ...] [--classification ...] [--page-size N] [--limit N]
c1i mcp tools approve <tool-id>... --app-id <id> --connector-id <id> [--state approved|disabled|pending]
c1i mcp tools delete  <tool-id> --app-id <id> --connector-id <id>
c1i mcp tools history <tool-id> --app-id <id> --connector-id <id> [--page-size N] [--limit N]

# Toolsets (admin-curated tool groupings; one AppEntitlement per toolset)
c1i mcp toolsets list                   --app-id <id> --connector-id <id> [--page-size N] [--limit N]
c1i mcp toolsets get                    <toolset-id> --app-id <id> --connector-id <id>
c1i mcp toolsets create                 --app-id <id> --connector-id <id> --display-name <name> [--description <text>]
c1i mcp toolsets update                 <toolset-id> --app-id <id> --connector-id <id> --display-name <name> [--description <text>]   # server 400s without --display-name: "DisplayName value length must be between 1 and 255 runes"
c1i mcp toolsets delete                 <toolset-id> --app-id <id> --connector-id <id>
c1i mcp toolsets get-by-entitlement     <app-entitlement-id> --app-id <id>
c1i mcp toolsets requestable-connectors <user-id>   # not paginated; returns the full set in one response

# Bindings (which tools belong to which toolset)
c1i mcp bindings list     --app-id <id> --connector-id <id> --toolset-id <tid> [--page-size N] [--limit N]
c1i mcp bindings create   --app-id <id> --connector-id <id> --toolset-id <tid> --tool-id <id> [--tool-id <id> ...]   # --tool-id max 100
c1i mcp bindings delete   --app-id <id> --connector-id <id> --toolset-id <tid> --tool-id <id> [--tool-id <id> ...]   # --tool-id max 100
c1i mcp bindings by-tools --app-id <id> --connector-id <id> --tool-id <id> [--tool-id <id> ...]   # --tool-id max 32
c1i mcp bindings history  --app-id <id> --connector-id <id> (--toolset-id <tid> | --tool-id <id>) [--page-size N] [--limit N]

# Classifiers (AI governance for MCP gateways)
c1i mcp classifiers list                           [--page-size N] [--page-token TOKEN] [--limit N]
c1i mcp classifiers get                            <classifier-id>
c1i mcp classifiers create                         --body-file classifier.json
c1i mcp classifiers update                         <classifier-id> --body-file classifier.json --update-mask <camelCase-fields>
c1i mcp classifiers delete                         <classifier-id>
c1i mcp classifiers bindings list                  [--page-size N] [--page-token TOKEN] [--limit N]
c1i mcp classifiers bindings create                --body-file binding.json
c1i mcp classifiers bindings delete                <binding-id>
c1i mcp classifiers templates list                 [--latest-only] [--page-size N] [--page-token TOKEN] [--limit N]
c1i mcp classifiers templates get                  <template-id> [--template-version <version>]
c1i mcp classifiers templates instantiate          <template-id> [--body-file params.json]
c1i mcp classifiers templates add-rule             <classifier-id> <template-id> [--body-file params.json]
c1i mcp classifiers policy show
c1i mcp classifiers policy update                  --body-file policy.json --update-mask <camelCase-fields>
c1i mcp classifiers tool-gates list                [--page-size N] [--page-token TOKEN] [--limit N]
c1i mcp classifiers tool-gates search              [--query <text>] [--page-size N] [--page-token TOKEN] [--limit N]
c1i mcp classifiers tool-gates get                 <tool-gate-id>
c1i mcp classifiers tool-gates create              --body-file tool-gate.json
c1i mcp classifiers tool-gates update              <tool-gate-id> --body-file tool-gate.json --update-mask <camelCase-fields>
c1i mcp classifiers tool-gates delete              <tool-gate-id>

# Gateway (verify end to end: list and invoke tools over the live MCP gateway)
c1i mcp gateway list-tools [--full] [--limit N] [--gateway-url <url>]
c1i mcp gateway call <tool-name> [--args '{"k":"v"}'] [--gateway-url <url>]
```

**`mcp classifiers`** manages the AI-governance API introduced for MCP
gateways. Named classifiers are reusable ordered rule cascades; bindings attach
one to an `AGENT` or `GATEWAY` target. `policy` is the separate, tenant-wide
agent policy. A rule with an empty `celCondition` matches every tool call, and
rules execute in order, so read the current object before changing `rules`.

`create` accepts a resource JSON object: `Classifier` for classifiers,
`ClassifierBinding` for bindings, and the full create request for tool gates.
`classifiers update` must include the classifier's required `displayName`,
even when the mask changes another field; read, edit, and resubmit the current
object. Every update requires `--update-mask`; include `rules` only when
deliberately replacing the complete cascade. `templates get` shows required
parameter keys. `instantiate` and `add-rule` accept an optional
JSON object with `templateVersion`, `params`, and, for `add-rule`, `ruleIndex`
or `insertIndex`; the positional IDs always select the template and
classifier. A tool gate's `filter` must select either `builtInPattern` or
`celExpression`, not both.

**Auth for `register` / `update-credentials` / `test-connection`:** convenience flags cover the simple methods — `--auth none`, `--auth bearer-token --bearer-token TOKEN`, `--auth custom-header --header-name NAME --header-value VALUE`, `--auth basic-auth --basic-auth-username USER --basic-auth-password PASS`. A blank companion value exits `2` before anything is previewed or sent, with two exceptions: `--token-sharing per-user` leaves the secret to each user (`SubmitUserCredential`), and on `update-credentials` / `test-connection` an explicit `--update-mask` requires only the arm fields whose path it names — a coarse path such as `basicAuth` requires both, and a mask naming none of the arm requires none, because the server carries the rest forward from the stored server. `register` has no mask, so it always needs the full arm (a custom header's name even under `per-user`). Token sharing is not readable back, so repeat `--token-sharing per-user` on `update-credentials` / `test-connection` when the stored server is per-user and you are not supplying the secret. For OAuth2 / AWS SigV4 / Google service-account auth, pass the full config object via `--hosted-config-file` / `--external-config-file` (JSON file, or `-` for stdin) — generate a ready-to-edit skeleton with `--print-config-template --auth <method> [--type hosted]` instead of hand-writing it. Secrets are sealed server-side; reads only ever return `*_configured` booleans, never the values. `--token-sharing shared|per-user` sets the server's token-sharing mode (case-insensitive; `per_user`/`peruser` are also accepted). Per the register help, `per-user` is only valid with `oauth2` in authorization-code or passthrough mode, `bearerToken`, `customHeader`, or `basicAuth`. Note that a read-back can legitimately differ from what you sent: the backend may store a *resolved* OAuth2 grant such as `..._MODE_AUTHORIZATION_CODE` in place of the input mode, so that is a normal round-trip, not a bug. `--source-app-id` names the source app for a connector-backed HOSTED server. `--data-sensitivity`, `--tool-prefix`, `--require-tool-approval` and `--user-id` (repeatable — sets the connector's integration owners) can all be set at `register` time, not only via `update`.

`mcp tools approve` is the standard post-registration step: newly discovered tools (from `register` or `resync-tools`) start in `PENDING_REVIEW`, and an admin approves them for the gateway to proxy calls. It takes one or more tool ids — the API has no batch approve, so each id is a separate request, but one invocation covers a whole toolset (pipe `mcp tools search --app-id <id> --connector-id <id> --state pending --fields id | jq -r .id`). It prints one compact JSON line per applied tool (`.id`, `.state`), even for a single id; a failed stdout write aborts the batch, and a partial failure exits with the first failure's code while earlier ids stay applied (under `--error-format json` read each per-id error's `applied` and the summary's `notAttempted`). The other `mcp` mutations print their response unwrapped like the matching `get` (a server's id is `.connectorId`, a toolset's, classifier's and tool gate's is `.id`; `mcp classifiers policy update` prints the singleton policy); deletes, `resync-tools` and `mcp bindings delete` print `{}`, and `mcp bindings create` prints `{"bindings":[…]}` as returned. `mcp servers update-credentials` without `--catalog-id` looks the catalog id up first and reports it on stderr as `Resolved catalog id: …`, also under `--dry-run`. History endpoints return records newest-first.

**`mcp gateway`** closes the configure-then-verify loop: after registering a server and approving its tools, `list-tools` runs the MCP handshake against the live gateway and shows what's actually callable, and `call` invokes a tool and prints its result. The gateway URL defaults to the `-mcp` host derived from `--url` (e.g. `acme.conductor.one` → `acme-mcp.conductor.one/v1`); override with `--gateway-url`. Your standard C1 token is accepted by the gateway, so no extra auth is needed. `call` always prints the full result, but exits `7` (not `0`) when the result itself reports `isError: true` — the call succeeded, the tool didn't.

### Access Requests

```sh
c1i requests create grant --app-id <id> --entitlement-id <eid> [--user-id <uid>] [--description <text>] [--duration <duration>] [--emergency]
c1i requests create revoke --app-id <id> --entitlement-id <eid> [--user-id <uid>] [--description <text>]
c1i requests list [--user-id <id> | --all] [--app-id <id>] [--entitlement-id <id>] [--state open|closed] [--type grant|revoke] [--page-size N] [--page-token TOKEN] [--limit N]
c1i requests get <request-id>
```

On `create`, `--user-id` defaults to the authenticated user when omitted.
`requests create grant --duration` takes a whole-number Go (`24h`) or protobuf
(`86400s`) duration, like `tasks update-grant-duration --duration`, and sends
it in seconds. No sign, no decimals: `-1h`, `1.5h` or `7d` exits `2`. `0s` is
sent, and the server refuses it with `value must be greater than 0s`.

`requests list` is the requester lens on access requests (the grant/revoke tasks
you file): by default it shows requests you opened or are the subject of —
complementing `tasks list`, which is the approver's My Work lens. Use `--user-id`
to scope to another user or `--all` for every request in the tenant. `--entitlement-id` requires `--app-id`: an entitlement id
repeats across apps, so the pair is the key. `requests
get` fetches a single request (the `.id` printed by `requests create`) as
pretty JSON, including its current policy step and outcome.
`requests create grant`/`revoke` print the created task unwrapped like `get`:
capture it with `jq -er .id`. Its `.state` is a snapshot from creation, not the
outcome.

### Findings

`findings` covers the public REST finding, governance-rule, settings, audit,
and shadow-MCP occurrence surfaces.

```sh
c1i findings search [--body-file <file|->] [--page-size N] [--page-token TOKEN] [--limit N]
c1i findings get <finding-id>
c1i findings create --body-file <file|->
c1i findings state <finding-id> --body-file <file|->
c1i findings assignee <finding-id> --body-file <file|->
c1i findings create-task <finding-id> [--policy-id <policy-id>]
c1i findings bulk-state --body-file <file|->
c1i findings bulk-create-tasks --body-file <file|->

c1i findings routing-rules list [--limit N]
c1i findings routing-rules get <rule-id>
c1i findings routing-rules create --body-file <file|->
c1i findings routing-rules update <rule-id> --body-file <file|->
c1i findings routing-rules delete <rule-id>
c1i findings transformation-rules list [--limit N]
c1i findings transformation-rules get <rule-id>
c1i findings transformation-rules create --body-file <file|->
c1i findings transformation-rules update <rule-id> --body-file <file|->
c1i findings transformation-rules delete <rule-id>
c1i findings settings show
c1i findings settings update --body-file <file|->
c1i findings audits search [--body-file <file|->] [--page-size N] [--page-token TOKEN] [--limit N]
c1i findings shadow-mcp-occurrences search --body-file <file|-> [--page-size N] [--page-token TOKEN] [--limit N]
```

The rule-list endpoints do not currently expose public filtering or paging; use
`--limit` only to cap local output. The upstream mapping gap is tracked in
[#131](https://github.com/ConductorOne/c1i/issues/131). Use JSON files for oneof actions, custom
finding targets, and nested rules; the commands insert positional IDs into the
request. `findings create-task` accepts an optional `--policy-id`; C1 otherwise
selects the app policy or built-in Finding Review policy. `bulk-state` and
`bulk-create-tasks` are asynchronous and return a `bulkActionId`; prefer
explicit `refs` in the JSON to avoid unexpectedly applying a broad
`searchRequest`.

Mutations print the finding or rule unwrapped like `get` (`jq -er .id`);
`routing-rules`/`transformation-rules` `delete` prints `{}`. The exceptions keep
the API's shape: `bulk-state`/`bulk-create-tasks` print `{"bulkActionId"}`,
`settings update` prints `{"list":[…]}`, and `create-task` prints
`{"finding":{…},"taskId":…}`, whose ids are `.taskId` (the created task) and
`.finding.id`.

The public API intentionally excludes its internal summary, app-NHI, and
shadow-MCP notification RPCs. `FindingSearchService.GetSearchTerms` is also not
routed through public REST; it is tracked in [#130](https://github.com/ConductorOne/c1i/issues/130).

### Role mining

`role-mining` manages the public role-mining analysis workflow, its suggested
access profiles, and tenant configuration.

```sh
c1i role-mining trigger
c1i role-mining suggestions list [--limit N]
c1i role-mining suggestions search --body-file <file|-> [--page-size N] [--page-token TOKEN] [--limit N]
c1i role-mining suggestions get <suggestion-id>
c1i role-mining suggestions state <suggestion-id> --body-file <file|->
c1i role-mining suggestions users <suggestion-id> --body-file <file|-> [--page-size N] [--page-token TOKEN] [--limit N]
c1i role-mining config show
c1i role-mining config update --body-file <file|->
c1i role-mining runs list [--limit N]
c1i role-mining runs latest
c1i role-mining custom-analysis list [--limit N]
c1i role-mining custom-analysis latest
c1i role-mining custom-analysis get <analysis-id>
c1i role-mining custom-analysis trigger --body-file <file|->
c1i role-mining custom-analysis evaluate <analysis-id> --body-file <file|->
c1i role-mining access-profiles create --body-file <file|->
```

`trigger` queues an organization analysis and returns an enqueue acknowledgement;
inspect `runs latest` for its eventual result. `custom-analysis trigger` returns
an analysis ID that `custom-analysis get` can poll. The latest custom-analysis
pointer is scoped to the authenticated user; custom-analysis list and get are
tenant-scoped. Mutations print the API's response: `suggestions state` the
suggestion (`.id`), `config update` the configuration (unwrapped, as
`config show` prints it), and `trigger` `{"runId"}`; `custom-analysis trigger`
prints `{"id"}` and `access-profiles create` `{"accessProfileId","celExpression"}`.
`config update` replaces the entire configuration, so begin with
`config show`. Access-profile creation persists the profile, entitlement bindings,
and optional automation as separate service operations; validate its JSON before
submitting it.

The suggestions, runs, and custom-analysis list routes do not accept paging or
state filters through public REST, despite returning a continuation token.
`--limit` only caps local output; the upstream mapping gap is tracked in
[#133](https://github.com/ConductorOne/c1i/issues/133). Organization overview,
legacy cohort analysis, coverage streaming, and the core MCP role-mining
service are not public REST routes.

### Access-review campaigns

Campaigns certify whether existing access should continue. The API calls the
resource an `access_review`, but c1i uses the UI's `access-reviews` name.

```sh
c1i access-reviews list [--page-size N] [--page-token TOKEN] [--limit N]
c1i access-reviews get <campaign-id>
c1i access-reviews create --body-file <file|->
c1i access-reviews update <campaign-id> --body-file <file|-> --update-mask <paths>
c1i access-reviews reports list <campaign-id> [--page-size N] [--page-token TOKEN] [--limit N]
c1i access-reviews reports generate <campaign-id> [--format json|csv|xlsx]
c1i access-reviews reports generate <campaign-id> --body-file <file|->
```

`create` takes the complete API request object. Its `scopeV2` is a deeply
nested oneof, so the CLI intentionally accepts it through `--body-file` rather
than a lossy flag surface; at least one `ownerIds` entry is required. `update`
takes a partial `AccessReview` object, inserts the id, and wraps it with the
required explicit `--update-mask`. Prepared campaigns cannot change their
scope or policy. `create` and `update` print the campaign unwrapped like `get`
(`jq -er .id`); `reports generate` prints `{}`.

Report generation is asynchronous: `generate` creates a report record but does
not return a ready download. Use `reports list` to find its state and
time-limited `downloadUrl`. Public API callers default to JSON when `--format`
is omitted; use `csv` or `xlsx` when needed. `--body-file` is for the full
generation request, including XLSX report-column configuration; it is mutually
exclusive with `--format`.

### Access profiles

An access profile controls which entitlements are requestable and who can
request them — admins use them to grant birthright access or to open access up
to a chosen audience.

**The API calls this object a request catalog**, and every path is
`/api/v1/catalogs`, so its JSON keys and ids say "catalog". The spec carries
both names — its `RequestCatalog` schema is tagged
`x-speakeasy-entity: Access_Profile` — so search for either.

Not to be confused with an app catalog, which is the per-user list of what one
user can request, derived from the access profiles they belong to.

```sh
c1i access-profiles list [--page-size N] [--page-token TOKEN] [--limit N]
c1i access-profiles get <access-profile-id>
c1i access-profiles create --display-name <name> [--description <text>] [--published] [--visible-to-everyone] [--request-bundle] [--enrollment-behavior bypass|enforce] [--unenrollment-behavior leave-access-as-is|revoke-all|revoke-unjustified] [--unenrollment-entitlement-behavior bypass|enforce]
c1i access-profiles update <access-profile-id> [--display-name <name>] [--description <text>] [--published] [--visible-to-everyone] [--request-bundle] [--enrollment-behavior bypass|enforce] [--unenrollment-behavior leave-access-as-is|revoke-all|revoke-unjustified] [--unenrollment-entitlement-behavior bypass|enforce]
c1i access-profiles delete <access-profile-id>

c1i access-profiles requestable-entitlements list <access-profile-id> [--page-size N] [--page-token TOKEN] [--limit N]
c1i access-profiles requestable-entitlements list-ids <access-profile-id>
c1i access-profiles requestable-entitlements add <access-profile-id> --app-id <id> --entitlement-id <id> [--entitlement-id <id> ...] [--create-requests]
c1i access-profiles requestable-entitlements remove <access-profile-id> --app-id <id> --entitlement-id <id> [--entitlement-id <id> ...]
c1i access-profiles requestable-entitlements set <access-profile-id> --refs-file <path>

c1i access-profiles visibility-entitlements list <access-profile-id> [--page-size N] [--page-token TOKEN] [--limit N]
c1i access-profiles visibility-entitlements add <access-profile-id> --app-id <id> --entitlement-id <id> [--entitlement-id <id> ...]
c1i access-profiles visibility-entitlements remove <access-profile-id> --app-id <id> --entitlement-id <id> [--entitlement-id <id> ...]

c1i access-profiles bundle-automation get <access-profile-id>
c1i access-profiles bundle-automation create <access-profile-id> --body-file <path>
c1i access-profiles bundle-automation set <access-profile-id> --body-file <path>
c1i access-profiles bundle-automation delete <access-profile-id>
c1i access-profiles bundle-automation resume <access-profile-id>
c1i access-profiles bundle-automation run <access-profile-id> [--refs-file <path>]
```

`access-profiles create` needs only `--display-name`. Every other field is
omitted unless explicitly passed; explicit `false` still reaches the API.
`update` derives its field mask from the flags supplied, so an explicit
`--description ""` clears it while omitted fields remain untouched. Behavior
flags accept short values shown above or the full `REQUEST_CATALOG_*` API enum.

`delete` is a soft delete: the catalog leaves `access-profiles list`, while
`access-profiles get` still returns it with `deletedAt` populated.

`create` and `update` print the profile unwrapped like `get` (`jq -er .id`;
the count is `.memberCount`). `bundle-automation create`/`set` print the
automation (id path `.requestCatalogId`); every delete, `resume`, `run` and
`requestable-entitlements`/`visibility-entitlements` mutation prints `{}`.

`requestable-entitlements` manages what the profile grants. `add` and `remove`
scope each entitlement to one `--app-id`; repeat `--entitlement-id` for that
application. `set` takes a JSON array of `{"appId":"…","id":"…"}` via
`--refs-file` (or `-` for stdin) and replaces the profile's entire requestable
set; use `[]` to clear it. `list-ids` returns the API's compact `{appId,id}`
reference list. `--create-requests` asks the server to create requests for
profile members when adding entries.

`visibility-entitlements` controls who can see the profile. Visibility additions
require a catalog that is published but not visible to everyone. The API returns
`catalog must be published to add an access entitlement` for an unpublished
catalog and `catalog is visible to everyone, cannot add access entitlements`
when `--visible-to-everyone` is set.

`bundle-automation` drives automatic bundled-entitlement grants. `create` and
`set` take a JSON object via `--body-file` (or `-` for stdin). The CLI validates
and sends `createTasks`, `disableCircuitBreaker`, `enabled`, and `entitlements`,
which contains an `entitlementRefs` array of `{"appId":"…","id":"…"}` objects.
`run --refs-file` takes that entitlement reference array and limits a run to
those references.

`access-profiles list` rows do **not** carry a member count: the list endpoint reports
`memberCount` as `0` for every catalog while `access-profiles get` on the same id
reports a non-zero count, so the key is omitted from list rows. `access-profiles get`
also carries the catalog's `accessEntitlements` (its visibility bindings),
empty when there are none, which list rows omit.

### Roles

Role ids are what `--scoped-role` takes on `service-principals credentials
create`; `auth login --scoped-role` also takes a role name (see
[Authentication](#authentication)).

```sh
c1i roles list [--page-size <n>] [--page-token <token>] [--limit <n>]
c1i roles get <role-id>
```

List rows carry `id`, `name` (e.g. `system:viewer`), `display_name`,
`system_builtin`, `system_api_only`, `created_at` and `updated_at`; `get` adds
the role's `serviceRoles` and `permissions`. A role id is 27 letters and
digits; a malformed one is rejected with `value does not match regex pattern
"^[a-zA-Z0-9]{27}$"` (exit 2).

### Service principals

A service principal (SPC) is a tenant-owned non-human identity. The principal is
just an identity; a **client credential** on it is what a caller authenticates
with (a `client_id`/secret pair), and a **binding** lets another subject — a
function, SSO application, AuthZEN server, or edge — act as the principal.

These first-class commands are the way to reach it; the raw `c1i api` escape
hatch works too.

```sh
c1i service-principals list [--page-size <n>] [--page-token <token>] [--limit <n>]
c1i service-principals get <sp-id>
c1i service-principals create --display-name <name>
c1i service-principals update <sp-id> --display-name <name>
c1i service-principals delete <sp-id>
```

`service-principals` is aliased to `sp`. `create` needs only `--display-name`;
the new principal is printed as pretty JSON, unwrapped like
`service-principals get`; read its id with `jq -er .id`. `delete` prints `{}`.

**Delete is destructive:** `service-principals delete` removes the principal
and every credential issued for it. Before deleting, inspect the principal with
`service-principals get <sp-id>`. Use `--dry-run` to preview the DELETE request
and target path; it does not verify that the target exists.

**Credentials** are how a service principal authenticates. The secret is returned
**once**, at creation, and cannot be retrieved again — capture it then.
`credentials create` prints the credential unwrapped, with the secret as a
sibling: `jq -er .id`, `.clientId` and `.clientSecret`. If writing that output
fails (a full disk, a closed pipe), the command exits `1` with `applied: true`
and says to revoke the credential and create a new one. `credentials update`
prints the credential; `revoke`, `bindings add` and `bindings delete` print `{}`.

```sh
c1i service-principals credentials list <sp-id>
c1i service-principals credentials get <credential-id> --service-principal-id <sp-id>
c1i service-principals credentials create <sp-id> --display-name <name> [--expires <duration>] [--scoped-role <role-id>] [--allow-cidr <cidr>] [--require-dpop]
c1i service-principals credentials update <credential-id> --service-principal-id <sp-id> --display-name <name>
c1i service-principals credentials revoke <credential-id> --service-principal-id <sp-id>
```

A principal's delete is reflected immediately: `service-principals get` exits
`4` and the lists omit it; `users get <sp-id>` shows `"status": "DELETED"` and
`deletedAt`.

Revoke is soft: `credentials get` on a revoked credential still returns the
record, exit 0, with no field marking it. Only the lists omit it.

`--expires` takes a whole-number Go (`720h`) or protobuf (`2592000s`)
duration (no sign, no decimals), sent in seconds; anything else, or an empty
one, exits `2`. The
server accepts the range `(0s, 4320h]` — up to 180 days — rejecting `0s` or
more with `value must be inside range (0s, 4320h0m0s]`.

`--scoped-role` and `--allow-cidr` are repeatable and restrict the credential to
those role ids / source CIDRs. Repeat either flag for multiple values. An empty
occurrence is a usage error before any request is sent. `--require-dpop` requires
DPoP proof-of-possession at token exchange. Only `--display-name` can be changed
after a credential is created.

**Bindings** (a draft API) name a subject that authenticates as the principal.
Exactly one subject is required; the SSO, AuthZEN, and edge subjects are
app-scoped, so they take both an id and an app id.

```sh
c1i service-principals bindings add --service-principal-id <sp-id> --function-id <fn-id>
c1i service-principals bindings add --service-principal-id <sp-id> --sso-application-id <id> --sso-app-id <app-id>
c1i service-principals bindings add --service-principal-id <sp-id> --authzen-server-id <id> --authzen-app-id <app-id>
c1i service-principals bindings add --service-principal-id <sp-id> --edge-id <id> --edge-app-id <app-id>
c1i service-principals bindings list --function-id <fn-id>
c1i service-principals bindings delete --service-principal-id <sp-id> --function-id <fn-id>
```

`bindings list` is by subject — it returns the principals a subject is bound to.
`add` and `delete` are idempotent.

### Webhooks

Outbound webhooks are the registered https endpoints C1.ai calls, for example
from a policy's webhook approval or provision step (which name one by
`webhookId`).

```sh
c1i webhooks list [--page-size N] [--page-token TOKEN] [--limit N]
c1i webhooks get <webhook-id>
c1i webhooks create --display-name NAME --destination-url https://hooks.example.com/c1 [--description TEXT] [--callback-timeout 10m]
c1i webhooks update <webhook-id> [--display-name NAME] [--destination-url URL] [--description TEXT] [--callback-timeout DURATION]
c1i webhooks test <webhook-id>
c1i webhooks delete <webhook-id>
```

The endpoint flag is `--destination-url`, not `--url`, which stays the global
tenant flag. It must be an absolute `https://` URL with a host name or IPv4
address; anything else exits `2` before a request is sent. The server would
otherwise rewrite another scheme, or none, to `https` on create, and reject it
on update with `invalid url: only 'https' scheme is supported`. An IPv6 literal
is refused because the server strips its brackets, and `webhooks test` then
fails with `invalid port ":db8::1" after host`. A host the server will not call
is a `400`, `invalid url: restricted destination` (exit `2`).

A blank `--display-name` exits `2` on create and update, as described under
[Display names](#display-names); the server would store one on update.

`--callback-timeout` takes a whole-number Go (`10m`) or protobuf (`600s`)
duration (no sign, no decimals) from `0` to `720h`, sent in seconds; `-1m` or
`1.5m` exits `2` before a request is sent; over `720h`, update returns `callback_timeout
must be between 0 and 30 days`.
`update` sends only the flags you pass and masks exactly those, and with no
field flag it exits `2`; an empty `--description` clears it. Updating a
soft-deleted webhook exits `0` and changes it, and it stays deleted. A webhook
has no signing secret, so create returns nothing that can't be read again with
`get`.

`webhooks list` pages are larger than `--page-size`: the server raises a size
below 5 to 5 and returns one row more than that, so use `--limit` for an exact
count.

`webhooks create` and `update` print the webhook, unwrapped (`jq -er .id`);
`webhooks delete` prints `{}`. `webhooks test` queues a delivery and returns
the queued instance with `"state": "WEBHOOK_STATE_PENDING"`, at exit `0`,
whether or not the URL is reachable. Its `.id` is the delivery and `.webhookId`
the webhook you tested. Exit `0` does not mean the endpoint answered.
`webhooks delete` is a soft delete: the webhook leaves `webhooks list`, but
`webhooks get` still returns it at exit `0` with `deletedAt` set, and deleting
it again exits `0`.

### Export

```sh
c1i export events [--since <rfc3339>] [--until <rfc3339>] [--since-event-uid <uid>] [--sort asc|desc] [--page-size N] [--page-token TOKEN] [--limit N]
```

`export events` dumps the C1 system log — OCSF-formatted audit events — as an
NDJSON stream (one event per line), auto-paginating through the whole result.
Redirect it to a file to archive events or ship them to an external system:

```sh
c1i export events > audit.ndjson                                  # everything, oldest first
c1i export events --since 2026-07-01T00:00:00Z --until 2026-07-08T00:00:00Z
c1i export events --since-event-uid <last-uid>                    # incremental sync
```

`--sort` defaults to `asc` (chronological), which pairs with `--since-event-uid`
for incremental sync. `--fields` works on events too (e.g. `--fields
activity_name,actor.user.email_addr,time`).

### Raw API

```sh
# GET request
c1i api --path /api/v1/apps

# POST request
c1i api --path /api/v1/search/users --body '{"pageSize":10}'

# Other methods — --method takes GET, POST, PUT, PATCH, or DELETE
c1i api --path /api/v1/apps/<app>/connectors/<conn>/mcp_tools/<id> --method DELETE

# DELETE normally refuses a body; some endpoints (e.g. remove-membership)
# require one, so opt in explicitly
c1i api --path /api/v1/apps/<app>/entitlements/<ent>/remove-membership \
  --method DELETE --body '{"appUserId":"<app-user>"}' --allow-delete-body

# Read the body from a file, or stdin with "-"
c1i api --path /api/v1/search/users --body-file query.json
echo '{"pageSize":10}' | c1i api --path /api/v1/search/users --body-file -

# Add query params and headers (both repeatable)
c1i api --path /api/v1/apps --query page_size=5 --header X-Request-Id=abc123

# Auto-paginate through all results (NDJSON output, one item per line)
c1i api --path /api/v1/apps --paginate

# Force the array field to drain when auto-detection picks the wrong one
c1i api --path /api/v1/automation_executions --paginate --list-key automationExecutions
```

The method defaults to GET, or POST when a body is set; pass `--method` for
PUT/PATCH/DELETE. The body comes from `--body` (inline JSON) or `--body-file` (a
file, or `-` for stdin) — the two are mutually exclusive. GET and DELETE refuse
a body by default (a body on either is more likely a mistake than intent); pass
`--allow-delete-body` to lift that for DELETE specifically, for the handful of
C1 endpoints that require one. `--query key=value` and
`--header key=value` are both repeatable. When `--paginate` is used, each page's first array-valued field is unwrapped and each item is emitted as a single line of NDJSON — the same format used by list commands. This covers both the canonical `list` key and typed keys like `automationExecutions`; use `--list-key <field>` to force a specific field (naming one that no page so far has carried, on a page that has other array fields, or one that is not an array, is a usage error, exit `2`). If the cursor stops advancing — the same `nextPageToken` sent twice, or two or more pages in a row, at least 25 items in all, holding only items already seen (compared by item `id`, else the item's canonical JSON) — `c1i` stops instead of looping, keeps the rows already printed (a page holding only items already seen is held, and printed once a later page brings a new item, the list ends, or a later request fails, so only a stuck run is dropped; id-less items that legitimately repeat across pages can trip this), warns on stderr that results are INCOMPLETE and names the route, and exits `6`. Without `--paginate`, the full JSON response is pretty-printed — and if that response carries a non-empty `nextPageToken`, `c1i` warns on stderr that the result is partial and names `--paginate`, since a truncated page is otherwise indistinguishable from a complete one at exit 0. stdout is unchanged either way.

### API Discovery & Documentation

The `docs` commands require no C1 credentials — agents can use them to explore the API before authenticating.

```sh
# Print the agent bootstrap doc: output contracts, exit codes, when to
# prefer first-class commands over raw API calls (write to a file with --output)
c1i docs agents [--output AGENTS.md]

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

# Print an embedded, task-oriented runbook (list names if omitted)
c1i docs guide
c1i docs guide register-mcp-server
```

`docs openapi`, `docs endpoints`, and `docs endpoint` use the current public API contract at `https://www.c1.ai/api/openapi.yaml`, including MCP server, tool, and toolset operations. The spec is cached for 24 hours at `~/.c1i/cache/api-openapi.yaml`; fetch failures fall back to that cache, however old, with a one-line warning on stderr naming its age. A response over 32 MiB counts as a failed fetch. With a cache to fall back on, a fetch makes one attempt unless you set `--max-retries` or `C1I_MAX_RETRIES`. With no cache, a failed fetch exits `4` for a `404`, `5` for a `429`, `6` for a `5xx`, and `8` for anything else, such as an unreachable host, a refused redirect or a redirect loop.

`docs search` is a semantic search with no relevance threshold: every query returns up to 10 nearest matches, so even a nonsense query comes back with plausible-looking hits. A returned hit is not proof a concept exists, and an unexpected hit is not proof the thing you searched for is absent — read the snippet, or fetch the page with `docs page`, to judge. To check whether an API endpoint exists, use `docs endpoints --filter`, which has a real no-match.

`docs guide` is embedded static content (no network call), unlike `docs search` / `docs page` which hit the C1 documentation site. Guides ship in two families: registering and operating MCP servers (`register-mcp-server`, `assign-toolset-everyone`, `test-mcp-gateway`) and everyday app/access-request workflows (`configure-new-app`, `request-access`, `inspect-and-approve-task`, `delegate-entitlement-provisioning`). Run `c1i docs guide` with no argument for the full, current list.

`docs skill` is kept as an alias of `docs agents` for backward compatibility;
both print identical output.

`~/.c1i/cache/` belongs to c1i: when `docs openapi`, `docs endpoints` or `docs endpoint` refreshes the spec, any file there that the running version doesn't use (such as one left by an older release) is deleted. Don't keep your own files in it.

## Output Conventions

- **List commands** (`users list`, `apps list`, etc.) output NDJSON (one JSON object per line).
- **Mutations** (create, update, delete, owner changes, task actions,
  bindings) print the API's response as pretty JSON, unwrapped the same way the
  matching `get` unwraps it; every other envelope key, `expanded` included,
  stays as a top-level sibling. **The id path varies by command:** most use
  `.id`, but an MCP server's is `.connectorId`, a proxy binding's `.srcAppId`,
  `role-mining trigger` prints `.runId`, `findings bulk-*` `.bulkActionId`,
  `role-mining access-profiles create` `.accessProfileId`,
  `findings create-task` `.taskId` and `.finding.id`, and `webhooks test`
  `.id` (the delivery) and `.webhookId`. Extract an id with `jq -er`, which
  fails rather than printing `null`. Deletes and other empty responses print
  `{}`; exit `0` is the success signal. A task's `.state` (`tasks` actions,
  `requests create`) is the server's snapshot, not the outcome: re-read with
  `tasks get`. `mcp tools approve` prints one compact JSON line per tool.
- **`api`** outputs pretty-printed JSON. With `--paginate`, outputs NDJSON (one list item per line). A `200` whose body is not JSON is an error (exit `6`), not a silent pass-through — a `--path` that escapes the API prefix can reach the web app and return HTML, which used to print as though it had succeeded. An empty body still succeeds, since some endpoints answer a write with nothing.
- **`docs`** commands output NDJSON (`search`, `endpoints`), pretty JSON (`endpoint`, `openapi` is YAML), or plain text (`page`).
- List commands auto-paginate by default, except the single-request ones the agent guide names under Pagination (the `role-mining` and `findings …-rules` lists warn on stderr when a result is partial). Pass `--page-token` with a non-empty cursor to fetch a single page manually; supplying the flag at all disables auto-pagination, so a blank value is a usage error (exit 2) rather than an empty filter. A page holding only rows already printed is skipped (apart from any rows without an id); a page with any new row is printed whole. If the server's cursor stops advancing, or sends 100 empty pages in a row (`export events` excepted), the command keeps the rows already printed, warns that results are INCOMPLETE, and exits `6`. A page whose rows all lack the id the command reads is an unrecognized response (exit `6`); commands keyed on several fields, such as `grants list` and `entitlements list`, print such rows instead.
- `--page-size` **requests** a per-call batch size (max 100; `mcp tools history` and `mcp bindings history` allow 200). It is not a guarantee: a page can contain more rows than you asked for, by an amount that varies per endpoint and per size — `apps list --page-size 10` returned 23 rows, `policies list` 12, `users list` exactly 10. A positive value below 5 usually returns 5, though `policies list` floors at 6 and `mcp servers catalog list` has no floor. `--page-size 0` means the server's default of 25, not "none". A value over the max is clamped by c1i rather than rejected. A negative `--page-size` or `--limit` is a usage error (exit 2), rejected before any request. Use `--limit N` for an exact total: it is enforced client-side, so it holds even when a page overshoots, and it stops auto-pagination once reached.

### Repeatable flags

A flag documented as **repeatable** takes one value per occurrence, and a comma
is a literal character rather than a separator:

```sh
c1i mcp bindings create --app-id A --connector-id C --toolset-id T \
  --tool-id tool-a --tool-id tool-b     # two tools
```

`--tool-id tool-a,tool-b` is one id containing a comma, not two ids. This is
easy to miss on `--config-field`, where `--config-field "region=us1,env=prod"`
sets `region` to `us1,env=prod` and the server may accept it.

An empty occurrence is a usage error (exit 2), rejected before any request, so
an unset shell variable cannot silently shorten a list or remove a credential
restriction. Contrast `--fields`, which *is* comma-separated.

### Enum flags

Every flag that takes a named value (`--state`, `--status`, `--type`,
`--policy-type`, `--sort`, `--auth`, `--unbind-policy`, `--error-format`,
`--method`, ...) parses it the same way: ASCII case and surrounding
whitespace are ignored, `-` and `_` are the same, and the full API name is
accepted in any case (`--state pending-review`, `--state
mcp_tool_state_approved`). Its `--help` lists the short names. An unknown or
non-ASCII value is a usage error (exit 2), rejected before any request, and so
is a whitespace-only value — it is not "no filter". Only omitting the flag
(or, for a filter, passing an empty string) leaves it unset.

Where `--help` says "or any `PREFIX_*` value", a value carrying that prefix
is sent upper-cased with `-` changed to `_`, so an enum newer than c1i still
works. Two flags whose API enums have no prefix, `users list --status` and
`entitlements create --resource-type`, send any ASCII value that way. The
only values rewritten to a different enum are these listed aliases:
`MCP_TOOL_STATE_PENDING` is sent as `MCP_TOOL_STATE_PENDING_REVIEW`, and `REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_BYPASS`/`_ENFORCE` as their `..._ENTITLEMENT_REQUEST_POLICY` names. No other prefixed value is ever remapped.

### Display names

A blank `--display-name` (empty, whitespace or invisible-only, such as a lone
U+200B) exits 2 on every create and update, before any request is sent.

### Field selection

`--fields` is the default for reducing JSON read output: use it when you know
the keys you need. It has no external dependency, keeps list output as NDJSON,
and understands c1i's snake_case/camelCase field variants.

Use `jq` only when the work is more than projection: conditional selection,
reshaping, aggregation, sorting, or extracting a value from mutation output
(with `jq -er`). `--fields` never changes mutation output.

```sh
# Known keys: prefer c1i's built-in projection
c1i users list --fields id,email

# Nested known keys: --fields preserves the object shape
c1i functions get <id> --fields id,displayName,publishedCommitId

# Conditional selection: jq is appropriate
DELETED_APPS="$(c1i apps list | jq -c 'if .deleted_at then . else empty end')"

# Mutation output: never projected; extract the id with jq -er
APP_ID=$(c1i apps create --display-name "Example" | jq -er .id)
```

- Comma-separated; use dot-paths (`user.email`) for nested access.
- Matches the emitted keys, trying an **exact match first**, then falling back
  to a **case- and separator-insensitive** match. So `--fields displayName`
  resolves whether the output uses `displayName` (single-object reads) or
  `display_name` (list rows); the output keeps the source key's own spelling.
- Single-object `get` commands print the resource itself, so `--fields id`
  yields `{"id": ...}` and `jq -r .id` works. The API wraps the resource under
  its own key (`app`, `function`, `userView.user`, ...); `get` unwraps that and
  keeps every other envelope key — `expanded` among them — as a top-level
  sibling. Do **not** write the wrapper into a path: `--fields function.id`
  no longer resolves, because there is no `function` key left.
- `c1i api` is a raw passthrough and still returns the envelope. There, and
  for any genuinely nested field, a name that doesn't match at the top level is
  also searched for deeper: the shallowest match wins, and a tie at the same
  depth resolves to the alphabetically first full path, deterministically.
- Mutation output is unwrapped the same way, but never projected: an explicit
  `--fields` on a mutation (even `--fields ""`) is a usage error (exit `2`),
  checked before anything is sent; `C1I_FIELDS` alone is ignored there. The id
  path varies by command (`.id`, `.connectorId`, `.srcAppId`, ...); a delete
  prints `{}`.
- A `--fields` spec that matches **nothing at all** in the response (a typo, or
  a field that truly doesn't exist) is a usage error (exit `2`), not a silent
  `{}`. This is a zero-match check only: `--fields id,dispalyName` (typo) still
  exits `0` and silently returns just `{"id": ...}` — the misspelled field is
  dropped with no error and no other signal that it didn't match anything. This
  is deliberate, not a gap in the check: `--fields`/`C1I_FIELDS` is a
  persistent, session-wide setting, so one spec is routinely applied across
  many differently-shaped responses; erroring on any unmatched name would make
  a session-wide `C1I_FIELDS` fail on every command whose response happens to
  lack one of the names. Double-check the spelling of every name you pass —
  the tool only catches getting *all* of them wrong.
  - On list commands and `api --paginate`, "the response" means the **whole
    result**, not each row: rows are decided and streamed out one at a time as
    they're fetched (never buffered — these commands can walk unbounded,
    multi-page results), but a row whose projection is empty is **skipped**
    rather than printed as `{}` — a field present on some rows but absent on
    others just means fewer rows are printed, not an error, as long as it
    matches at least one row anywhere. Only a spec that matches nothing in
    **every** row across every page is an error, with a message ("...matched
    no keys in any row of the response") distinguishable from the
    single-object case above. Because every row's own projection was already
    empty (and skipped) by the time that whole-result verdict is known,
    nothing is printed on stdout before the error — never rely on output
    length to detect success without checking the exit code. If you're piping
    to `jq` (`c1i ... | jq ...`), remember `$?` reads the pipe's last command,
    not `c1i`'s — the stderr message is the durable signal here, not the exit
    code you'd read from a naive `$?` after the pipe.
  - **Worst case: a `--fields` spec that matches nothing at all, paired with
    `--limit`, scans the collection to completion before erroring** — "nothing
    ever matched" can't be known short of exhausting every page, and a typo is
    the ordinary way to hit this. Live-measured: `tasks list --fields <typo>
    --limit 2` made 193 requests over ~41s before exiting `2` on a tenant with
    ~9,650 tasks; on a 35,000-row `entitlements list` that's minutes. This
    isn't specific to `--fields` — it's the same rule `accounts list
    --unmapped-only` and `functions usage` already follow for their own
    client-side filtering (a filter applied after the fetch can't bound the
    work when nothing matches, no matter what `--limit` says); `--fields`
    combined with `--limit` is one more instance of it, not a new behavior.
- Missing fields are silently omitted, so requesting a superset is safe.
- Also settable via `C1I_FIELDS`. Applies to read output — list commands, `api`,
  and single-object `get` commands. Mutation output is never projected, so a
  session-wide `C1I_FIELDS` is ignored there.

### Errors & exit codes

On failure, `c1i` writes an error to stderr and exits with a code an agent can
branch on without parsing text:

| Code | Meaning |
|------|---------|
| `0` | success |
| `1` | generic / unclassified error |
| `2` | usage error (bad flags or arguments, an empty, `.` or `..` id, a blank required or id flag or positional, a missing, malformed, `null` or wrong-shape `--*-file` input, a non-object element in an array `--*-file` or `--body-file`, a refused redirect, or the API returned any `4xx` other than `401`/`403`/`404`/`408`/`429`/`499`) |
| `3` | not authenticated, or API returned `401`/`403` |
| `4` | API returned `404` (not found) |
| `5` | API returned `429` (rate limited — back off and retry) |
| `6` | C1 failed: the API returned `5xx`, a redirect chain never settled, it answered `200` with a body that isn't JSON, a list command's pagination cursor stopped advancing, or a list page in a shape the command doesn't recognize |
| `7` | `mcp gateway call` completed, but the tool itself reported an error (`isError: true` in its result) |
| `8` | a system beyond C1, or the MCP protocol layer, failed — an upstream connector was unreachable or errored, the gateway itself was unreachable (DNS failure, refused connection), `mcp servers test-connection` reported an EXTERNAL server unreachable (its JSON is still printed), or the gateway returned a protocol-level JSON-RPC error |

Exit `1` with `writing output:` means stdout failed (e.g. a full disk). On a
mutation the message is `<METHOD> <path> succeeded but writing its response
failed`: the change was applied, so verify before retrying a mutation.

`6` and `8` are both "something remote broke," but they call for different
responses. `6` means C1 itself is failing, so retrying later is reasonable and
there is nothing to fix at your end. `8` means C1 answered fine and something
past it did not, so the same call will usually fail the same way: if a connector
is at fault, inspect it (`c1i mcp servers get <connector-id> --app-id <id>`, and
`mcp servers test-connection` for an EXTERNAL server) — it may be unreachable or
its credentials may have expired; if it's a protocol-level JSON-RPC error, that
indicates a version mismatch or a bug in c1i, worth reporting rather than
working around. Before this split both arrived as `6`, which made "C1 is down"
indistinguishable from "the Slack connector is down."

An **empty id argument** is a usage error, not a lookup: `c1i policies get ""`
exits `2` and sends nothing. Without that guard an empty id renders a trailing
empty path segment, which the API redirects to the collection endpoint — so the
command used to print the entire list and exit `0`. An id of `.` or `..` is
refused the same way, since the API resolves it to the parent or collection,
as is one with a `;` suffix (`..;x`), which the API treats as `..`. The same
check applies to a raw `api --path` ending in `/` or containing such a segment,
percent-encoded (`%2E`) or not.

Relatedly, the REST client is **selective about HTTP redirects**. It follows one
only when both hold: the target path is identical to what was requested (a
trailing-slash difference counts as a change), and the target host is in the same
trust scope as the request host — the same host differing only in scheme or port,
or a `label.`-prefix relationship in either direction with at least two labels,
which covers `apex ↔ www` canonicalization. An `https`→`http` downgrade is never
followed. Anything else — a different path, or the same path on an unrelated host
— is refused as an error (exit `2`) naming the target.

Both halves matter. The path rule is what closed a real bug: an id of `/` or `.`
escapes to a path the API redirects to the collection, which turned a
single-object read into a full listing with exit `0`. The host rule is what keeps
your token safe: a followed redirect is re-authenticated, so an unrestricted
follow would hand your bearer token to whatever host the redirect named.

A chain of allowed redirects that doesn't settle within five hops fails as a
remote error (exit `6`) rather than looping.

This applies to every command that sends HTTP: the REST client, the MCP
gateway, the login handshake, `upgrade` and every `docs` command, so the path
and redirect guards, `--debug` tracing, and `--max-retries` cover all of them,
not just REST commands (`upgrade` and the `docs` commands map a refused
redirect to exit `8`, and `docs search`/`docs page` map any failure to `8`).

One narrower carve-out inside login: the device-flow token poll forces its own
retry count to zero, because RFC 8628's polling interval already *is* that
call's retry strategy and a second layer underneath would double the delays.
`--max-retries` still governs the rest of the handshake.

A bad id is the only cause of a refused `3xx` observed so far, which is why it
maps to exit `2` — a redirect on an otherwise well-formed request would not be
the caller's mistake, and would still report `2`.

Pass `--error-format json` (or `C1I_ERROR_FORMAT=json`) to get a machine-readable
error object instead of the default `Error: <msg>` line. For API errors it
includes the status, method, path, and response body:

```sh
$ c1i api --path /api/v1/apps/<nonexistent-id> --error-format json
{"body":{"code":5,"message":"not found (request-id: ...)"},"error":"API error: API GET /api/v1/apps/<nonexistent-id> returned 404: ...","method":"GET","path":"/api/v1/apps/<nonexistent-id>","status":404}
```

The `body` is embedded as JSON when the API returned JSON, otherwise as a string.

In JSON mode **stderr is NDJSON**: every line is one object. Errors carry
`"error"` (select them with `jq 'select(.error)'`), warnings and other notices
`"notice"` and `"kind"`, `--wait` progress `"progress"`, and `--debug` trace
lines `"debug"`. Text mode is unchanged.

An error from a command that writes also carries `"applied"` (`api` does not:
c1i can't tell whether an arbitrary call writes):

| `applied` | When |
|---|---|
| `false` | Nothing reached the server: a flag or argument error, an explicit `--fields`, a refused id or redirect, a failed pre-send lookup, a failure before the request was written, or an API `4xx` other than `408`/`499` |
| `"unknown"` | The request may have reached the server: an API `408`/`499` or `5xx`, a transport error or timeout after the request was written, or **any** failure after an earlier attempt was sent and retried (a `DELETE` that gets a `503` and then a `404` is `"unknown"`) |
| `true` | The change was applied: the response wasn't JSON, printing it failed, or a `--wait` after it failed |

`entitlements create` adds a `"steps"` array naming each attempted request, and
its `applied` is `true` once any step was written.

`mcp tools approve` reports each failed id as its own object
(`{"error":…,"id":"<tool-id>","applied":…}`) and ends with a summary object
that has no `id`:
`{"error":"approved N of M tools; …","applied":…,"succeeded":N,"failed":K,"notAttempted":[…]}`.
**A batch can exit non-zero with changes applied:** it exits with the first
failure's code while the ids before it stay approved, a failed stdout write
stops it (exit `1`), and `notAttempted` lists the ids it never sent.

Exit codes are unchanged.

## Configuration

c1i requires a C1 **URL**.

**c1i requires `https`.** A URL with any other scheme is rejected (exit `2`); it
is not rewritten. Credentials embedded in the URL are dropped with a warning on
stderr, and an embedded password is never echoed.

The scheme may be omitted (`tenant.c1eu.ai`), in which case `https` is assumed.
The host is lower-cased, so `HTTPS://TENANT.C1EU.AI` and `tenant.c1eu.ai`
resolve identically, and a protocol-relative `//tenant.example` is handled.

Both `*.conductor.one` and `*.c1eu.ai` (EU) tenant domains are accepted — pass
whichever your tenant uses. Only the shape of the URL is checked, never the
domain, so a typo like `mycompany.conductor.on` is accepted here and surfaces
later as an authentication failure.

A bare name (`--url mycompany`) is rejected: with more than one tenant domain in
use it is ambiguous, and silently expanding it to `mycompany.conductor.one` would
point an EU tenant at the wrong region. The error names where the value came
from, which matters when it is a stale entry in `~/.c1i.yaml` rather than
something you just typed. A single-label host is fine as long as it arrives as a
URL (`https://c1-staging`), which is how an internal-resolver name is reached.

> If you previously authenticated with a mixed-case `--url`, your stored
> credential was keyed by that exact casing and is no longer found now that the
> host is normalized. Run `c1i auth login` once to re-store it.

Set it via (in order of precedence):

1. `--url` flag
2. `C1I_URL` environment variable
3. `~/.c1i.yaml` config file:

   ```yaml
   url: https://mycompany.conductor.one
   ```

   A terminal `c1i auth login` given the URL another way offers to save it
   there. If the file isn't a valid YAML mapping, login warns and leaves it
   unchanged. With a relative `HOME`, c1i neither reads nor writes it, so a
   `.c1i.yaml` in the working directory is never used.

These are equivalent:
- `--url https://mycompany.conductor.one`
- `--url mycompany.conductor.one`
- `--url MYCOMPANY.CONDUCTOR.ONE` (the host is lower-cased)

**Every command prints which tenant it's about to use when the URL came from
`~/.c1i.yaml`.** Nothing on the command line names the config file, so a
stale entry there sends a command to the wrong tenant with no visible sign
otherwise. `--url` and `C1I_URL` don't print this warning: both are explicit
choices made for that invocation, and warning on every normal use of
`C1I_URL` would just train you to stop reading it. The warning goes to
stderr, once per invocation (never once per page of a paginated list):

```
Warning: no --url flag given; targeting https://mycompany.conductor.one (from ~/.c1i.yaml)
```

For credential storage, see [Credential sources](#credential-sources) below.

### Retries

Transient API failures are retried automatically with exponential backoff and
jitter, honoring a `Retry-After` header when the server sends one. This keeps
long auto-paginated pulls from failing on a single rate-limit blip. What gets
retried depends on the request, to avoid duplicating side effects:

- **`429 Too Many Requests`** — retried for every command (the request is
  rejected before the server processes it, so a retry is always safe).
- **Transient `5xx` (500, 502, 503, 504) and network errors** — retried only for
  idempotent reads and updates (GET/PUT/DELETE) and for the read-only search and
  list `POST`s behind list commands (`/api/v1/search/*`, and any `/api/v1/` path
  ending in `/search` or `/list`). Other `POST` mutations
  (e.g. `requests create`, `tasks approve`) are **not** retried on these, since
  the server may have already applied the change before the failure.
  Non-transient 5xx (501 Not Implemented, 505, 511) are never retried.

Control the retry budget (attempts *after* the first try) via, in order of
precedence:

1. `--max-retries N` flag (any command that reaches the C1 API)
2. `C1I_MAX_RETRIES` environment variable
3. Default: `4`

Set `--max-retries 0` to disable retries entirely. Non-retryable responses
(4xx other than 429, and 501/505) fail immediately.

A non-empty `C1I_*` value that doesn't parse (a boolean such as
`C1I_DRY_RUN`, or `C1I_MAX_RETRIES`) exits `2` naming the variable before any
request; the flag given on the command line overrides the variable entirely.

This covers the token mint too: minting or refreshing the OAuth2 bearer
c1i authenticates with is itself a request, subject to the same 429 retry
(never 5xx/network, since it's a POST) and the same `--max-retries` budget.

Every request also gets a fixed timeout, per attempt (a retried request
gets a fresh budget, not a shrinking share of one deadline): 10 minutes
for a REST or MCP gateway request, 30 seconds for `auth login`'s
device-flow requests and the OAuth2 token mint/refresh above — tighter
because those are fast request/response exchanges, not the kind of call
that legitimately runs long. Neither is configurable. 10 minutes leaves
roughly 3x headroom over the longest request this CLI is known to make
(an MCP `tools/call` invoking a slow tool, observed at 182 seconds), so
there's no known case that needs a longer one.

### Dry run

`--dry-run` (or `C1I_DRY_RUN=1`) previews a C1 REST mutation and returns
without sending it. The preview is one JSON object on stdout, the requests in
send order with the full URL and the exact body (`upgrade --dry-run` is the
exception: it prints plain `[dry-run]` text lines, since it sends no C1 API
mutation):

```sh
$ c1i requests create grant --app-id A1 --entitlement-id E1 --user-id U1 --dry-run
{
  "dryRun": true,
  "requests": [
    {
      "method": "POST",
      "url": "https://tenant.example.invalid/api/v1/task/grant",
      "body": {
        "appEntitlementId": "E1",
        "appId": "A1",
        "identityUserId": "U1"
      }
    }
  ]
}
```

Its only keys are `dryRun` and `requests` (one entry per request: several for
`mcp tools approve` with several ids, up to three for `entitlements create`);
it never has an `id`, and `body` is omitted when the request has none. A
stderr notice, `dry run: nothing was sent`, accompanies every preview.

It applies to every REST write command and to non-GET `api` calls, and never
sends that mutation. `mcp gateway call` rejects both `--dry-run` and
`C1I_DRY_RUN`: c1i cannot preview or suppress a gateway tool's side effects, so
inspect the tool and treat its invocation as live. `auth login` rejects both
too: it can't preview or suppress storing a credential.

A preview resolves the URL first, so it needs one configured (exit `2`
without), and it applies the same id guard as a real send: an empty, `.` or
`..` id exits `2`. An explicit `--fields` exits `2` too, since a preview is
never projected. A preview needs no credentials, except where c1i must look
something up so the previewed body is exact:

- `tasks approve`, `deny`, `restart`, `skip-step` and `reassign` without
  `--policy-step-id` read the task to resolve its current policy step;
- `mcp servers update-credentials` for a hosted server without `--catalog-id`
  reads the server to resolve its catalog id;
- `requests create grant`/`revoke` without `--user-id` resolve the caller;
- `upgrade` fetches and verifies the release (see Upgrading).

Other pre-send checks, such as the policy lookups on `apps` and
`entitlements` writes, are skipped on a preview. `policies update` skips its
policy-type lookup too, so a patch that needs the type exits `2` under
`--dry-run` unless you pass `--policy-type`.

**An inherited `C1I_DRY_RUN=1` turns every write in a script into a
preview.** The preview has no `id`, so `jq -r .id` prints `null` and the script
carries on with it. Extract ids with `jq -er`, which fails on a preview:

```sh
APP_ID=$(c1i apps create --display-name "Example" | jq -er .id) || exit 1
```

### Debug tracing

`--debug` (or `C1I_DEBUG=1`) traces each API HTTP request to stderr — method,
URL, response status, and elapsed time, including every retry attempt. Headers
and bodies are never logged, so credentials don't leak. Output goes to stderr,
so it won't corrupt piped JSON on stdout:

```sh
$ c1i apps list --debug 2>trace.log
$ cat trace.log
> GET https://mycompany.conductor.one/api/v1/apps
< GET /api/v1/apps 200 OK (142ms)
```

## Authentication

```sh
# Browser-based login (OAuth device flow)
c1i auth login

# Limit the browser-login credential to chosen roles
c1i auth login --choose-roles                  # pick from a menu after approving
c1i auth login --scoped-role basic-user        # repeatable; a role name or id

# Or store credentials directly
c1i auth login --client-id <id> --client-secret <secret>

# Check credential status (also reports the storage backend)
c1i auth status

# Show the authenticated principal (principle/user ID, role/permission/feature counts, and
# display name + email when a best-effort secondary lookup succeeds) plus the resolved
# tenant: "tenant" (base URL) and "tenantSource" (flag/env/config)
c1i auth whoami
# --verbose swaps the summary for the raw introspect payload (full roles/permissions/
# features arrays, but no display name or email) -- a different projection, not a superset
c1i auth whoami --verbose

# Machine-readable "which tenant am I about to write to?" — the pre-write check
c1i auth whoami --url https://mycompany.conductor.one --fields tenant

# Mint a short-lived bearer token for driving raw API calls yourself
c1i auth token            # add --json for token type and absolute expiry (RFC3339)

# Remove stored credentials
c1i auth logout
```

Browser login creates a personal client credential with all of your roles. To
limit it, pass `--choose-roles` (a menu after you approve in the browser; needs
a terminal) or `--scoped-role <role>` (repeatable). The two can't be combined,
and neither applies to `--client-id`/`--client-secret` (exit 2). When no
credential is stored for the tenant, a terminal login asks which you want:
Enter keeps all of your roles, and `--choose-roles=false` skips the question.

- **Which roles.** A scoped credential keeps only the overlap between its roles
  and your own access. The menu and role names offer the roles you hold, Basic
  User, and Read-Only Administrator (a read-only view of your access); holders
  of Super Administrator or Read-Only Administrator see every role. In the
  menu, enter numbers or `0` for all of your roles; a blank answer asks again.
- **Names and IDs.** A role ID is 27 letters and digits and goes to C1 as
  given; an unknown one exits 4. Anything else is a name: case is ignored, and
  spaces, underscores and hyphens are interchangeable. A name that matches no
  offered role, or more than one, exits 2 before your credential is created,
  listing the choices.
- **Checks.** Login verifies the new credential before storing it. If C1
  scoped it wider than asked (exit 6), a scoped credential has no access
  (exit 2), verification or storing fails, or you press Ctrl-C, login deletes
  it. Scoped credentials are named `Created by c1i (<role or count>)`.
- **The temporary credential.** The device-flow token can only create a
  credential, so reading roles uses a short-lived one with all of your roles,
  deleted before yours is created. If it isn't deleted, login names it. If
  yours was stored, login exits 1; otherwise it exits with the login's own
  failure code and stores nothing.
- A browser login that replaces a stored credential names it; it stays active
  in C1.ai.

`c1i auth token` prints just the access token, newline-terminated, so it
composes into `curl -H "Authorization: Bearer $(c1i auth token)" ...`. It is
never written to disk — a new one is minted per invocation.

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
the active credentials. If the config directory resolves to a relative path,
from a relative `HOME`, `%AppData%` or `XDG_CONFIG_HOME`, the file fallback
fails with an error and the token cache below skips its file, rather than
writing under the working directory.

### Token cache

To avoid minting a fresh OAuth token on every invocation — each mint writes a
`client_credentials` event to the tenant's audit log — `c1i` caches the access
token and reuses it until it nears expiry. It uses the OS keyring when
available. On Unix-like hosts without a usable keyring, it instead uses a
hardened `0600` file under the config directory (`~/.config/c1i/tokens/` on
Linux). A cached token the server rejects (clock skew, or a revoked credential)
is dropped and re-minted once automatically. Each file-cache write also deletes
the other token files there that c1i would refuse to load (expired or expiring
within a minute, corrupt or unreadable, or open to group or other users) and
temp files over a minute old.
The token is strictly shorter-lived than the client secret already stored
beside it. Set `C1I_NO_TOKEN_CACHE=1` to disable caching and mint per
invocation.

## Shell Completion

```sh
# bash
mkdir -p ~/.local/share/bash-completion/completions
c1i completion bash > ~/.local/share/bash-completion/completions/c1i

# zsh
c1i completion zsh > "${fpath[1]}/_c1i"

# fish
c1i completion fish > ~/.config/fish/completions/c1i.fish
```


```powershell
# PowerShell (load in the current session)
c1i completion powershell | Out-String | Invoke-Expression
```

Each generator takes `--no-descriptions` to emit a script that completes names
only, without the per-command help text.

## Version

```sh
c1i version       # or: c1i --version
```

## Upgrading

```sh
c1i upgrade                       # upgrade to the latest stable release (prompts first)
c1i upgrade --check               # print a JSON report; change nothing
c1i upgrade --channel latest -y   # take the newest release without prompting
```

`upgrade` follows a release channel from the C1.ai distribution center
(`dist.conductorone.com`): `stable` by default, or `latest`/`preview` via
`--channel`. It replaces a standalone binary only after verifying the release
manifest's Sigstore signature (signed by the release workflow run for that c1i
tag, recorded in Rekor) and the download's SHA-256 from that manifest.
`--yes`/`-y` skips the prompt and is required when stdin is not a terminal.
`--check` prints `current`, `latest`, `channel`, `update_available`,
`install_method` (`standalone`, `homebrew`, `go-install`, `container`, `system`,
or `windows`), and, for a Homebrew, `go install`, or container install,
`upgrade_command`. For those installs `upgrade` prints that command and exits 0
without replacing anything; Windows and system installs print a hint but no
`upgrade_command`. A source build (`dev` or a Go pseudo-version) is never
replaced: `upgrade` prints the channel's release and exits 0, and `--check`
reports `update_available: false`. `--dry-run` fetches and verifies the release (index,
manifest, signature, TUF root) and checks the install directory is writable,
then prints what it would download and replace; it takes no lock, doesn't
prompt, and installs nothing.

`upgrade` treats the distribution center as a system beyond C1: a dist `404`,
`429`, or `5xx` exits `4`, `5`, or `6`, but
any other dist `4xx`, a refused redirect, a redirect loop, a non-JSON `200`, or a TUF, signature,
or checksum failure exits `8`.

The channel list (`index.json`) and its yank flags are not signed, so a
compromised distribution origin could withhold upgrades or offer any signed
release newer than yours, yanked or prerelease, but never an unsigned build or
one older than the one you run.

## Uninstalling

Remove the binary the way you installed it: `brew uninstall c1i` for
Homebrew; for `go install`, delete `c1i` from `GOBIN` (set in the environment
or with `go env -w`), or from `$GOPATH/bin` (`~/go/bin` by default) when
`GOBIN` is unset; a downloaded binary is wherever you put it; and
`docker rmi public.ecr.aws/conductorone/c1i:<version>` for the container image.

Removing the binary leaves these behind:

- `~/.c1i/`, the OpenAPI spec cache.
- `~/.c1i.yaml`, the config file.
- `<UserConfigDir>/c1i/`, holding the file-fallback credentials
  (`credentials/`) and the access-token file cache (`tokens/`). See
  [Credential sources](#credential-sources) for each OS's config directory;
  on Linux it is `$XDG_CONFIG_HOME` when that is set.
- OS keyring entries: credentials under `c1i/<host>`, and cached access tokens
  (including tokens for `C1I_CLIENT_ID`/`C1I_CLIENT_SECRET` credentials) under
  `com.conductorone.c1i.tokens`. Older releases stored a
  `<name>.conductor.one` tenant's credentials under `c1i/<name>`.
- Any [shell completion](#shell-completion) script you installed, and the
  `c1i completion powershell` line if you added it to your PowerShell profile.

c1i can't list keyring entries or keep track of the tenants you've logged in to.
To remove its keyring credentials, run `auth logout` once per tenant before you
remove the binary. If `C1I_CLIENT_ID` and `C1I_CLIENT_SECRET` are both set, unset
them first: logout still removes the stored credentials, but drops the cached
token for the env-var credentials instead of theirs.

```sh
c1i auth logout --url example.conductor.one
```

Logout also drops the cached token for the credentials stored now. A token
cached under credentials you since replaced stays in the keyring. To remove
keyring entries by hand, replace `<host>` below with a tenant's host, such as
`example.conductor.one`, or with `<name>` for an entry an older release stored:

```sh
# macOS: each call deletes one item; repeat until it reports "could not be found"
security delete-generic-password -s 'c1i/<host>'
security delete-generic-password -s com.conductorone.c1i.tokens

# Linux (Secret Service): each call deletes every matching item
secret-tool clear service 'c1i/<host>'
secret-tool clear service com.conductorone.c1i.tokens
```

On Windows, open Credential Manager → Windows Credentials and remove
`c1i/<host>:client_id`, `c1i/<host>:client_secret`, and every
`com.conductorone.c1i.tokens:` entry. To find which hosts you have, search for
`c1i` in Keychain Access (macOS), Seahorse (Linux) or Credential Manager.

`~/.sigstore/root/`, which `upgrade` uses to verify releases, is shared with
other Sigstore tools, so leave it in place.

## License

Apache 2.0
