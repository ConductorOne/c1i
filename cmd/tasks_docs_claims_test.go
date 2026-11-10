package cmd

import (
	"strings"
	"testing"
)

// TestResetReopensClosedTaskIsDocumented pins the two user-facing claims a
// closed task turns on: "reset" reopens it (exit 0, state OPEN, outcome
// cleared) and "restart" does not (the server answers "task is closed", exit
// 2). Both docs said neither reopened, which would steer an agent away from the
// only working undo.
func TestResetReopensClosedTaskIsDocumented(t *testing.T) {
	docs := map[string]string{
		"README.md":                readDocFile(t, "../README.md"),
		"cmd/agents.md (embedded)": agentsTemplate,
		"tasks reset --help":       tasksResetCmd.Long,
	}

	for name, raw := range docs {
		text := flatten(raw)
		if strings.Contains(text, "Neither reopens a closed task") {
			t.Errorf("%s still claims neither action reopens a closed task", name)
		}
		if !strings.Contains(text, "reopen") {
			t.Errorf("%s does not state that reset reopens a closed task", name)
		}
	}
	for name, raw := range map[string]string{
		"README.md":                readDocFile(t, "../README.md"),
		"cmd/agents.md (embedded)": agentsTemplate,
	} {
		if !strings.Contains(flatten(raw), "reopens a closed task") {
			t.Errorf("%s does not state that reset reopens a closed task", name)
		}
	}

	restart := flatten(tasksRestartCmd.Long)
	if strings.Contains(restart, "leaves the state CLOSED") {
		t.Error("tasks restart --help still claims a closed task stays CLOSED")
	}
	if !strings.Contains(restart, "task is closed") {
		t.Error(`tasks restart --help does not quote the server's "task is closed"`)
	}
	if !strings.Contains(flatten(agentsTemplate), "`task is closed`") {
		t.Error("cmd/agents.md does not quote the server's `task is closed` for restart")
	}
}

// TestGrantDurationDocsNameNestedPath pins the read-back path. The value is
// stored at .type.grant.grantDuration; the top-level .grantDuration is null,
// so a read that trusts the old wording silently sees nothing.
func TestGrantDurationDocsNameNestedPath(t *testing.T) {
	const nested = ".type.grant.grantDuration"
	docs := map[string]string{
		"README.md":                          readDocFile(t, "../README.md"),
		"cmd/agents.md (embedded)":           agentsTemplate,
		"tasks update-grant-duration --help": tasksUpdateGrantDurationCmd.Long,
	}
	for name, raw := range docs {
		if !strings.Contains(raw, nested) {
			t.Errorf("%s does not name the nested task path %s", name, nested)
		}
	}
	for name, raw := range map[string]string{
		"cmd/agents.md (embedded)": agentsTemplate,
		"README.md":                readDocFile(t, "../README.md"),
	} {
		if strings.Contains(flatten(raw), "lands as `grantDuration`") {
			t.Errorf("%s still claims the value lands as a top-level grantDuration", name)
		}
	}
}
