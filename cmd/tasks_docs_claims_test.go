package cmd

import (
	"strings"
	"testing"
)

// TestClosedTaskReopenBehaviorIsDocumented pins the measured closed-task
// matrix: neither action is a general undo, and the outcome depends on how the
// task closed. reset reopens CANCELLED/GRANTED/REVOKED closes but exits 0 and
// leaves a DENIED one CLOSED; restart reopened a GRANTED close and is refused
// with "task is closed" on CANCELLED and DENIED ones. The docs used to say
// neither reopened, then that only reset did; both steer an agent wrong.
func TestClosedTaskReopenBehaviorIsDocumented(t *testing.T) {
	docs := map[string]string{
		"README.md":                readDocFile(t, "../README.md"),
		"cmd/agents.md (embedded)": agentsTemplate,
	}

	for name, raw := range docs {
		text := flatten(raw)
		if strings.Contains(text, "Neither reopens a closed task") {
			t.Errorf("%s still claims neither action reopens a closed task", name)
		}
		if !strings.Contains(text, "DENIED") {
			t.Errorf("%s does not carve out the denial-closed case", name)
		}
		if !strings.Contains(text, "reopen") {
			t.Errorf("%s does not state that reset/restart can reopen a closed task", name)
		}
	}

	reset := flatten(tasksResetCmd.Long)
	if !strings.Contains(reset, "DENIED") || !strings.Contains(reset, "reopen") {
		t.Error("tasks reset --help does not state that a DENIED-closed task does not reopen")
	}
	if !strings.Contains(reset, "tasks get") {
		t.Error("tasks reset --help does not send the reader to re-read with tasks get")
	}

	restart := flatten(tasksRestartCmd.Long)
	if strings.Contains(restart, "does NOT reopen a closed task") {
		t.Error("tasks restart --help still claims a closed task never reopens")
	}
	if strings.Contains(restart, "leaves the state CLOSED") {
		t.Error("tasks restart --help still claims a closed task always stays CLOSED")
	}
	if !strings.Contains(restart, "task is closed") {
		t.Error(`tasks restart --help does not quote the server's "task is closed"`)
	}
	if !strings.Contains(restart, "GRANTED") {
		t.Error("tasks restart --help omits the GRANTED close it did reopen")
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
