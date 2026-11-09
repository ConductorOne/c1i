Run: `go run .` (from the repo root)

The authoritative guide (tenant/auth, global flags, output contracts, exit
codes, pagination, when to prefer a first-class command over raw `api`) is
`cmd/agents.md`; print it with `go run . docs agents`. For any command or flag,
`go run . --help` and `go run . <group> --help` are the source of truth.

Don't restate either here: an earlier copy of the flag facts and exit-code table
drifted from the code. `TestSkillFileDefersToAgentsDoc` enforces this.

## Changing c1i

`CLAUDE.md` is binding: run its full gate list and its dependency scan before a
PR, and keep commit messages and PR text free of tenant names, internal repo
names and ticket IDs.
