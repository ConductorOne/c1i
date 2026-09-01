package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// stubNewClient points the shared newClient seam at an httptest server,
// bypassing the OAuth mint. Mirrors stubPoliciesClient (policies_update_test).
func stubNewClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	orig := newClient
	newClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newClient = orig })
}

// runTaskActionCmd drives one task-action command's RunE against srv and
// returns its stdout.
func runTaskActionCmd(t *testing.T, cmd *cobra.Command, srv *httptest.Server, taskID string) (string, error) {
	t.Helper()
	stubNewClient(t, srv)
	withRealDryRun(t)
	t.Setenv("C1I_URL", "https://example.invalid")

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetContext(context.Background())
	err := cmd.RunE(cmd, []string{taskID})
	return out.String(), err
}

// taskActionResponse is the action endpoints' success shape, with the caller's
// choice of echoed state.
func taskActionResponse(taskID, state string) string {
	return fmt.Sprintf(`{"taskView":{"task":{"id":%q,"state":%q}},"expanded":[]}`, taskID, state)
}

const closeTestTaskID = "zz-c1i-test-task-1"

// TestTasksCloseNeverEchoesResponseState is the load-bearing test for this
// command pair: /action/close returns the task as it was BEFORE the close, so
// a live close of an open task answers TASK_STATE_OPEN. Printing that state
// would tell the user the close didn't happen. The confirmation must carry the
// id only.
func TestTasksCloseNeverEchoesResponseState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/tasks/"+closeTestTaskID+"/action/close" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// The pre-mutation state, exactly as the live API echoes it.
		_, _ = fmt.Fprint(w, taskActionResponse(closeTestTaskID, "TASK_STATE_OPEN"))
	}))
	defer srv.Close()

	resetCmdFlags(t, tasksCloseCmd)
	out, err := runTaskActionCmd(t, tasksCloseCmd, srv, closeTestTaskID)
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	// Exact match, not a substring absence: a relabelled state ("was=open")
	// would slip past a TASK_STATE/state= check.
	if want := "Closed task: task_id=" + closeTestTaskID + "\n"; out != want {
		t.Errorf("output = %q, must not report a state: the response echoes the pre-close state", out)
	}
}

// TestTasksCloseBodyOmitsEmptyComment pins the request shape: an unset
// --comment must be left out entirely, not sent as "".
func TestTasksCloseBodyOmitsEmptyComment(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("server: unmarshaling request body: %v", err)
		}
		_, _ = fmt.Fprint(w, taskActionResponse(closeTestTaskID, "TASK_STATE_OPEN"))
	}))
	defer srv.Close()

	resetCmdFlags(t, tasksCloseCmd)
	if _, err := runTaskActionCmd(t, tasksCloseCmd, srv, closeTestTaskID); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if len(gotBody) != 0 {
		t.Errorf("body = %v, want {} when --comment is unset", gotBody)
	}
}

func TestTasksCloseSendsComment(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = fmt.Fprint(w, taskActionResponse(closeTestTaskID, "TASK_STATE_OPEN"))
	}))
	defer srv.Close()

	resetCmdFlags(t, tasksCloseCmd)
	_ = tasksCloseCmd.Flags().Set("comment", "no longer needed")
	if _, err := runTaskActionCmd(t, tasksCloseCmd, srv, closeTestTaskID); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if gotBody["comment"] != "no longer needed" {
		t.Errorf("body = %v, want comment to be sent verbatim", gotBody)
	}
}

// TestTasksReassignSendsUserIDsAsArray pins the reassign wire shape:
// newStepUserIds is a JSON array of strings (a repeated proto field), never a
// single comma-joined string, and policyStepId rides alongside it.
func TestTasksReassignSendsUserIDsAsArray(t *testing.T) {
	const taskID = "zz-c1i-test-task-2"

	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/tasks/"+taskID+"/action/reassign" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("server: unmarshaling request body: %v", err)
		}
		_, _ = fmt.Fprint(w, taskActionResponse(taskID, "TASK_STATE_OPEN"))
	}))
	defer srv.Close()

	resetCmdFlags(t, tasksReassignCmd)
	_ = tasksReassignCmd.Flags().Set("to-user-id", "user-a")
	_ = tasksReassignCmd.Flags().Set("to-user-id", "user-b")
	_ = tasksReassignCmd.Flags().Set("policy-step-id", "step-1")

	out, err := runTaskActionCmd(t, tasksReassignCmd, srv, taskID)
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}

	ids, ok := gotBody["newStepUserIds"].([]any)
	if !ok {
		t.Fatalf("newStepUserIds = %#v, want a JSON array", gotBody["newStepUserIds"])
	}
	if len(ids) != 2 || ids[0] != "user-a" || ids[1] != "user-b" {
		t.Errorf("newStepUserIds = %v, want [user-a user-b]", ids)
	}
	if gotBody["policyStepId"] != "step-1" {
		t.Errorf("policyStepId = %v, want step-1", gotBody["policyStepId"])
	}
	if _, sent := gotBody["comment"]; sent {
		t.Errorf("body = %v, want no comment key when --comment is unset", gotBody)
	}
	if want := "Reassigned task: task_id=" + taskID + " policy_step_id=step-1\n"; out != want {
		t.Errorf("output = %q, want exactly %q: the response echoes the pre-reassign state", out, want)
	}
}

// TestTasksReassignDerivesCurrentPolicyStep proves the flagless path: with no
// --policy-step-id, the task is fetched and its currently executing step
// (taskView.task.policy.current.id) is what the reassign posts.
func TestTasksReassignDerivesCurrentPolicyStep(t *testing.T) {
	const taskID = "zz-c1i-test-task-3"
	const currentStep = "zz-c1i-test-step-current"

	var gotBody map[string]any
	var sawGet bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/tasks/"+taskID:
			sawGet = true
			_, _ = fmt.Fprintf(w, `{"taskView":{"task":{"policy":{"current":{"id":%q}}}}}`, currentStep)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/tasks/"+taskID+"/action/reassign":
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &gotBody)
			_, _ = fmt.Fprint(w, taskActionResponse(taskID, "TASK_STATE_OPEN"))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	resetCmdFlags(t, tasksReassignCmd)
	_ = tasksReassignCmd.Flags().Set("to-user-id", "user-a")

	if _, err := runTaskActionCmd(t, tasksReassignCmd, srv, taskID); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if !sawGet {
		t.Error("expected a GET of the task to derive the current policy step")
	}
	if gotBody["policyStepId"] != currentStep {
		t.Errorf("policyStepId = %v, want the current step %q", gotBody["policyStepId"], currentStep)
	}
}

// TestTasksReassignUnderivableStepIsUsageError: reassignment always applies to
// a step, so a task with no current step must fail with the actionable
// "pass --policy-step-id" usage error (exit 2) rather than posting without one.
func TestTasksReassignUnderivableStepIsUsageError(t *testing.T) {
	const taskID = "zz-c1i-test-task-4"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			t.Errorf("must not post a reassign with no policy step: %s", r.URL.Path)
		}
		_, _ = fmt.Fprint(w, `{"taskView":{"task":{"policy":{}}}}`)
	}))
	defer srv.Close()

	resetCmdFlags(t, tasksReassignCmd)
	_ = tasksReassignCmd.Flags().Set("to-user-id", "user-a")

	_, err := runTaskActionCmd(t, tasksReassignCmd, srv, taskID)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if got, want := exitCode(err), exitUsage; got != want {
		t.Errorf("exitCode(%v) = %d, want %d (exitUsage)", err, got, want)
	}
	if !strings.Contains(err.Error(), "--policy-step-id") {
		t.Errorf("error = %q, want it to name --policy-step-id", err.Error())
	}
}
