package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const actionTestTaskID = "zz-c1i-test-task-2"

// taskActionRecorder answers every action POST with a success body and records
// the path and body it received.
type taskActionRecorder struct {
	srv    *httptest.Server
	paths  []string
	bodies []map[string]any
}

func newTaskActionRecorder(t *testing.T, state, currentStepID string) *taskActionRecorder {
	t.Helper()
	r := &taskActionRecorder{}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// A GET is the policy-step lookup resolvePolicyStepID performs.
		if req.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"taskView":{"task":{"id":"` + actionTestTaskID +
				`","state":"` + state + `","policy":{"current":{"id":"` + currentStepID + `"}}}}}`))
			return
		}
		raw, _ := io.ReadAll(req.Body)
		var body map[string]any
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Errorf("decoding body for %s: %v", req.URL.Path, err)
			}
		}
		r.paths = append(r.paths, req.URL.Path)
		r.bodies = append(r.bodies, body)
		_, _ = w.Write([]byte(taskActionResponse(actionTestTaskID, state)))
	}))
	t.Cleanup(r.srv.Close)
	return r
}

// TestTaskActionsPostTheirOwnVerb pins that each command hits its own action
// path. Sharing one RunE makes a copied verb the plausible mistake, and the
// wrong verb would silently perform a different action on the task.
func TestTaskActionsPostTheirOwnVerb(t *testing.T) {
	cases := []struct {
		cmdName  string
		wantPath string
	}{
		{"restart", "/api/v1/tasks/" + actionTestTaskID + "/action/restart"},
		{"reset", "/api/v1/tasks/" + actionTestTaskID + "/action/reset"},
		{"skip-step", "/api/v1/tasks/" + actionTestTaskID + "/action/skip-step"},
		{"process", "/api/v1/tasks/" + actionTestTaskID + "/action/process"},
		{"close", "/api/v1/tasks/" + actionTestTaskID + "/action/close"},
		{"approve", "/api/v1/tasks/" + actionTestTaskID + "/action/approve"},
		{"deny", "/api/v1/tasks/" + actionTestTaskID + "/action/deny"},
	}
	for _, tc := range cases {
		t.Run(tc.cmdName, func(t *testing.T) {
			cmd := findTasksSubcommand(t, tc.cmdName)
			resetCmds(t, cmd)
			r := newTaskActionRecorder(t, "TASK_STATE_OPEN", "zz-step-1111111111111111111")
			if _, err := runTaskActionCmd(t, cmd, r.srv, actionTestTaskID); err != nil {
				t.Fatalf("%s: %v", tc.cmdName, err)
			}
			if len(r.paths) != 1 {
				t.Fatalf("%s posted %d times, want 1: %v", tc.cmdName, len(r.paths), r.paths)
			}
			if r.paths[0] != tc.wantPath {
				t.Errorf("%s posted %q, want %q", tc.cmdName, r.paths[0], tc.wantPath)
			}
		})
	}
}

// TestTaskActionsSendPolicyStepOnlyWhenTheyUseOne pins the three step modes.
// Sending policyStepId to an endpoint that takes none, or omitting it where the
// server requires it, are both silent-wrong-request failures.
func TestTaskActionsSendPolicyStepOnlyWhenTheyUseOne(t *testing.T) {
	const step = "zz-step-1111111111111111111"
	cases := []struct {
		cmdName  string
		wantStep bool
	}{
		{"restart", true},
		{"skip-step", true},
		{"approve", true},
		{"reset", false},
		{"process", false},
		{"close", false},
	}
	for _, tc := range cases {
		t.Run(tc.cmdName, func(t *testing.T) {
			cmd := findTasksSubcommand(t, tc.cmdName)
			resetCmds(t, cmd)
			r := newTaskActionRecorder(t, "TASK_STATE_OPEN", step)
			if _, err := runTaskActionCmd(t, cmd, r.srv, actionTestTaskID); err != nil {
				t.Fatalf("%s: %v", tc.cmdName, err)
			}
			got, ok := r.bodies[0]["policyStepId"]
			if tc.wantStep {
				if !ok || got != step {
					t.Errorf("%s body policyStepId = %v (present=%v), want %q", tc.cmdName, got, ok, step)
				}
				return
			}
			if ok {
				t.Errorf("%s sent policyStepId=%v to an endpoint that takes none", tc.cmdName, got)
			}
		})
	}
}

// TestTaskActionsNeverEchoResponseState extends the guarantee close already
// had to every action that does not intend to print state: these endpoints
// return the task as it was BEFORE the action, so echoing it reports the old
// state as though the action had not happened.
func TestTaskActionsNeverEchoResponseState(t *testing.T) {
	for _, name := range []string{"restart", "reset", "skip-step", "process", "close", "comment", "update-grant-duration"} {
		t.Run(name, func(t *testing.T) {
			cmd := findTasksSubcommand(t, name)
			resetCmds(t, cmd)
			if name == "comment" {
				_ = cmd.Flags().Set("comment", "zz")
			}
			if name == "update-grant-duration" {
				_ = cmd.Flags().Set("duration", "3600s")
			}
			r := newTaskActionRecorder(t, "TASK_STATE_OPEN", "zz-step-1111111111111111111")
			out, err := runTaskActionCmd(t, cmd, r.srv, actionTestTaskID)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if strings.Contains(out, "TASK_STATE_OPEN") {
				t.Errorf("%s echoed the pre-action state: %q", name, out)
			}
			if !strings.Contains(out, actionTestTaskID) {
				t.Errorf("%s did not report the task id: %q", name, out)
			}
		})
	}
}

// TestTasksRestartOmitsEmptyPolicyStepField pins that a closed task, which has
// no current step, does not produce "policy_step_id=" with nothing after it.
func TestTasksRestartOmitsEmptyPolicyStepField(t *testing.T) {
	cmd := findTasksSubcommand(t, "restart")
	resetCmds(t, cmd)
	r := newTaskActionRecorder(t, "TASK_STATE_CLOSED", "")
	out, err := runTaskActionCmd(t, cmd, r.srv, actionTestTaskID)
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	if strings.Contains(out, "policy_step_id=\n") || strings.HasSuffix(strings.TrimRight(out, "\n"), "policy_step_id=") {
		t.Errorf("restart printed an empty policy_step_id field: %q", out)
	}
}

// TestTasksUpdateGrantDurationRequiresDuration pins the usage error rather than
// letting the server answer "value is required" after a round trip.
func TestTasksUpdateGrantDurationRequiresDuration(t *testing.T) {
	cmd := findTasksSubcommand(t, "update-grant-duration")
	resetCmds(t, cmd)
	_ = cmd.Flags().Set("duration", "")
	r := newTaskActionRecorder(t, "TASK_STATE_OPEN", "zz-step-1111111111111111111")
	_, err := runTaskActionCmd(t, cmd, r.srv, actionTestTaskID)
	if err == nil {
		t.Fatal("expected a usage error for an empty --duration")
	}
	if got := exitCode(err); got != exitUsage {
		t.Errorf("exitCode = %d, want %d (exitUsage); err = %v", got, exitUsage, err)
	}
	if len(r.paths) != 0 {
		t.Errorf("a request was sent despite the usage error: %v", r.paths)
	}
}

// findTasksSubcommand looks the command up in the real tree, so a command that
// stops being registered fails here instead of silently going untested.
func findTasksSubcommand(t *testing.T, name string) *cobra.Command {
	t.Helper()
	for _, c := range tasksCmd.Commands() {
		if c.Name() == name {
			return c
		}
	}
	t.Fatalf("tasks has no %q subcommand", name)
	return nil
}
