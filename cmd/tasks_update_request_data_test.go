package cmd

import (
	"net/http"
	"testing"
)

// The file's object is sent as data, nothing else but what the endpoint takes.
func TestTasksUpdateRequestDataSendsTheFileAsData(t *testing.T) {
	cmd := findTasksSubcommand(t, "update-request-data")
	resetCmds(t, cmd)
	_ = cmd.Flags().Set("data-file", "testdata/task_request_data.json")
	r := newTaskActionRecorder(t, "TASK_STATE_OPEN", "zz-step-1111111111111111111")
	if _, err := runTaskActionCmd(t, cmd, r.srv, actionTestTaskID); err != nil {
		t.Fatalf("update-request-data: %v", err)
	}
	if len(r.bodies) != 1 {
		t.Fatalf("posted %d times, want 1", len(r.bodies))
	}
	data, ok := r.bodies[0]["data"].(map[string]any)
	if !ok || data["zz_field"] != "zz" || len(r.bodies[0]) != 1 {
		t.Errorf("body = %v, want only data from the file", r.bodies[0])
	}
}

// A missing, non-object or null file exits 2 before any request.
func TestTasksUpdateRequestDataBadFileExits2(t *testing.T) {
	for name, args := range map[string][]string{
		"missing": {"t1"},
		"array":   {"t1", "--data-file", "@[]"},
		"null":    {"t1", "--data-file", "@null"},
	} {
		t.Run(name, func(t *testing.T) {
			for i, a := range args {
				if len(a) > 1 && a[0] == '@' {
					args[i] = writeTempJSON(t, "data.json", a[1:])
				}
			}
			t.Setenv("C1I_URL", "https://example.invalid")
			s := newMutServer(t, fixedReply(http.StatusOK, `{}`))
			r := runMutationCLI(t, s.srv, 0, nil, append([]string{"tasks", "update-request-data"}, args...)...)
			if r.exit != exitUsage {
				t.Fatalf("exit = %d, want %d; stderr %q", r.exit, exitUsage, r.stderr)
			}
			if len(s.seen) != 0 {
				t.Errorf("sent %d request(s), want none", len(s.seen))
			}
		})
	}
}
