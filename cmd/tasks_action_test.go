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

// taskActionExpectations pins, per action command, the path it must POST and
// whether its body carries policyStepId. Seeded against tasksCmd.Commands()
// by TestEveryTaskActionIsPinned, so adding a command without adding a row
// here fails rather than going silently untested.
var taskActionExpectations = map[string]struct {
	verb     string
	wantStep bool
	// setup supplies flags the command requires before it will run.
	setup func(cmd *cobra.Command)
}{
	"approve":               {verb: "approve", wantStep: true},
	"deny":                  {verb: "deny", wantStep: true},
	"close":                 {verb: "close", wantStep: false},
	"restart":               {verb: "restart", wantStep: true},
	"reset":                 {verb: "reset", wantStep: false},
	"skip-step":             {verb: "skip-step", wantStep: true},
	"process":               {verb: "process", wantStep: false},
	"comment":               {verb: "comment", wantStep: false, setup: func(c *cobra.Command) { _ = c.Flags().Set("comment", "zz") }},
	"update-grant-duration": {verb: "update-grant-duration", wantStep: false, setup: func(c *cobra.Command) { _ = c.Flags().Set("duration", "3600s") }},
	"reassign":              {verb: "reassign", wantStep: true, setup: func(c *cobra.Command) { _ = c.Flags().Set("to-user-id", "zz-user") }},
}

// nonActionTaskSubcommands are the tasks subcommands that are not action POSTs.
var nonActionTaskSubcommands = map[string]bool{"list": true}

// TestEveryTaskActionIsPinned is the guard on the guard: every action command
// in the tree must have a row above.
func TestEveryTaskActionIsPinned(t *testing.T) {
	seen := 0
	for _, c := range tasksCmd.Commands() {
		name := c.Name()
		if nonActionTaskSubcommands[name] {
			continue
		}
		seen++
		if _, ok := taskActionExpectations[name]; !ok {
			t.Errorf("tasks %s has no row in taskActionExpectations, so nothing pins its action path or policy-step behaviour", name)
		}
	}
	if seen == 0 {
		t.Fatal("found no task action commands — this guard is not looking at what it thinks it is")
	}
	for name := range taskActionExpectations {
		if findTasksSubcommandOrNil(name) == nil {
			t.Errorf("taskActionExpectations lists %q, which is no longer a tasks subcommand", name)
		}
	}
}

// TestEveryTaskActionPostsItsOwnVerbAndStep drives every pinned command and
// checks both the path and whether policyStepId is on the wire. A copied verb
// would perform a different action on the task while printing success.
func TestEveryTaskActionPostsItsOwnVerbAndStep(t *testing.T) {
	const step = "zz-step-1111111111111111111"
	for name, want := range taskActionExpectations {
		t.Run(name, func(t *testing.T) {
			cmd := findTasksSubcommand(t, name)
			resetCmds(t, cmd)
			if want.setup != nil {
				want.setup(cmd)
			}
			r := newTaskActionRecorder(t, "TASK_STATE_OPEN", step)
			if _, err := runTaskActionCmd(t, cmd, r.srv, actionTestTaskID); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if len(r.paths) != 1 {
				t.Fatalf("%s posted %d times, want 1: %v", name, len(r.paths), r.paths)
			}
			if got, wantPath := r.paths[0], "/api/v1/tasks/"+actionTestTaskID+"/action/"+want.verb; got != wantPath {
				t.Errorf("%s posted %q, want %q", name, got, wantPath)
			}
			got, ok := r.bodies[0]["policyStepId"]
			if want.wantStep {
				if !ok || got != step {
					t.Errorf("%s body policyStepId = %v (present=%v), want %q", name, got, ok, step)
				}
				return
			}
			if ok {
				t.Errorf("%s sent policyStepId=%v to an endpoint that takes none", name, got)
			}
		})
	}
}

// TestTasksCommentAlwaysSendsTheCommentKey pins the one action whose empty
// value must still reach the wire: the comment IS the payload, so an omitted
// key records nothing while the command still prints success.
func TestTasksCommentAlwaysSendsTheCommentKey(t *testing.T) {
	cmd := findTasksSubcommand(t, "comment")
	resetCmds(t, cmd)
	_ = cmd.Flags().Set("comment", "")
	r := newTaskActionRecorder(t, "TASK_STATE_OPEN", "zz-step-1111111111111111111")
	if _, err := runTaskActionCmd(t, cmd, r.srv, actionTestTaskID); err != nil {
		t.Fatalf("comment: %v", err)
	}
	got, ok := r.bodies[0]["comment"]
	if !ok || got != "" {
		t.Errorf("comment body = %v (present=%v), want an empty string present", got, ok)
	}
}

// TestTasksDenyOmitsAnUnresolvableStep pins deny's stepOptional mode on the
// wire: when the current step cannot be derived the field must be absent, not
// empty, and the denial must still go through.
func TestTasksDenyOmitsAnUnresolvableStep(t *testing.T) {
	cmd := findTasksSubcommand(t, "deny")
	resetCmds(t, cmd)
	r := newTaskActionRecorder(t, "TASK_STATE_OPEN", "") // no current step
	if _, err := runTaskActionCmd(t, cmd, r.srv, actionTestTaskID); err != nil {
		t.Fatalf("deny: %v", err)
	}
	if got, ok := r.bodies[0]["policyStepId"]; ok {
		t.Errorf("deny sent policyStepId=%v when no step could be resolved; the field must be omitted", got)
	}
	if len(r.paths) != 1 {
		t.Errorf("deny posted %d times, want 1", len(r.paths))
	}
}

// findTasksSubcommandOrNil is findTasksSubcommand without the fatal, for the
// reverse direction of the pinning check.
func findTasksSubcommandOrNil(name string) *cobra.Command {
	for _, c := range tasksCmd.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
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
