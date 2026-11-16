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

// TestTasksClosePrintsTheServerSnapshot: /action/close returns the task as it
// was BEFORE the close, so a live close of an open task answers
// TASK_STATE_OPEN. c1i prints that response untouched (it neither relabels nor
// hides the state); the docs tell the reader to re-read with "tasks get".
func TestTasksClosePrintsTheServerSnapshot(t *testing.T) {
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
	want := "{\n  \"id\": \"" + closeTestTaskID + "\",\n  \"state\": \"TASK_STATE_OPEN\",\n  \"expanded\": []\n}\n"
	if out != want {
		t.Errorf("output = %q, want the unwrapped response %q", out, want)
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
		// Reassign checks each --to-user-id before the action.
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/users/") {
			_, _ = fmt.Fprint(w, `{"id":"user"}`)
			return
		}
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
	want := "{\n  \"id\": \"" + taskID + "\",\n  \"state\": \"TASK_STATE_OPEN\",\n  \"expanded\": []\n}\n"
	if out != want {
		t.Errorf("output = %q, want the unwrapped response %q", out, want)
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
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/users/"):
			_, _ = fmt.Fprint(w, `{"id":"user"}`)
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
// re-read-the-task usage error (exit 2) rather than posting without one.
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
	if !strings.Contains(err.Error(), "no current policy step to target") || !strings.Contains(err.Error(), "c1i tasks get "+taskID) {
		t.Errorf("error = %q, want the re-read-the-task message", err.Error())
	}
}

// reassignTestTaskJSON is the step lookup's response: one current step.
func reassignTestTaskJSON(taskID, stepID string) string {
	return fmt.Sprintf(`{"taskView":{"task":{"id":%q,"policy":{"current":{"id":%q}}}}}`, taskID, stepID)
}

// TestTasksReassignRefusesAnUnknownUser: the API accepts any 27-char id as the
// step's new approver and leaves the task stranded, so an unknown user must
// fail before the action is sent.
func TestTasksReassignRefusesAnUnknownUser(t *testing.T) {
	const taskID = "zz-c1i-test-task-6"
	const unknown = "ZZZZZZZZZZZZZZZZZZZZZZZZZZZ"

	var userGets []string
	var posts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/users/"):
			userGets = append(userGets, strings.TrimPrefix(r.URL.Path, "/api/v1/users/"))
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"code":5,"message":"not found"}`)
		case r.Method == http.MethodGet:
			_, _ = fmt.Fprint(w, reassignTestTaskJSON(taskID, "zz-c1i-test-step"))
		default:
			posts = append(posts, r.URL.Path)
			_, _ = fmt.Fprint(w, taskActionResponse(taskID, "TASK_STATE_OPEN"))
		}
	}))
	defer srv.Close()

	resetCmdFlags(t, tasksReassignCmd)
	_ = tasksReassignCmd.Flags().Set("to-user-id", unknown)

	_, err := runTaskActionCmd(t, tasksReassignCmd, srv, taskID)
	if err == nil {
		t.Fatal("reassign accepted an unknown user")
	}
	if got, want := exitCode(err), exitUsage; got != want {
		t.Errorf("exitCode = %d, want %d (exitUsage); err = %v", got, want, err)
	}
	if !strings.Contains(err.Error(), unknown) || !strings.Contains(err.Error(), "no such user") {
		t.Errorf("error = %q, want it to name the unknown --to-user-id", err.Error())
	}
	if len(userGets) != 1 || userGets[0] != unknown {
		t.Errorf("user lookups = %v, want one GET for %s", userGets, unknown)
	}
	if len(posts) != 0 {
		t.Errorf("reassign sent an action despite the unknown user: %v", posts)
	}
}

// TestTasksReassignDeduplicatesUserIDs: a repeated id is looked up once and
// sent once; a duplicate in the body reaches the server as a repeated approver.
func TestTasksReassignDeduplicatesUserIDs(t *testing.T) {
	const taskID = "zz-c1i-test-task-7"
	const userA = "aaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const userB = "bbbbbbbbbbbbbbbbbbbbbbbbbbb"

	var userGets []string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/users/"):
			userGets = append(userGets, strings.TrimPrefix(r.URL.Path, "/api/v1/users/"))
			_, _ = fmt.Fprint(w, `{"id":"user"}`)
		case r.Method == http.MethodGet:
			_, _ = fmt.Fprint(w, reassignTestTaskJSON(taskID, "zz-c1i-test-step"))
		default:
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &gotBody)
			_, _ = fmt.Fprint(w, taskActionResponse(taskID, "TASK_STATE_OPEN"))
		}
	}))
	defer srv.Close()

	resetCmdFlags(t, tasksReassignCmd)
	_ = tasksReassignCmd.Flags().Set("to-user-id", userA)
	_ = tasksReassignCmd.Flags().Set("to-user-id", userB)
	_ = tasksReassignCmd.Flags().Set("to-user-id", userA)

	if _, err := runTaskActionCmd(t, tasksReassignCmd, srv, taskID); err != nil {
		t.Fatalf("reassign: %v", err)
	}
	wantGets := []string{userA, userB}
	if len(userGets) != len(wantGets) || userGets[0] != wantGets[0] || userGets[1] != wantGets[1] {
		t.Errorf("user lookups = %v, want one per distinct id %v", userGets, wantGets)
	}
	ids, ok := gotBody["newStepUserIds"].([]any)
	if !ok {
		t.Fatalf("newStepUserIds = %#v, want a JSON array", gotBody["newStepUserIds"])
	}
	if len(ids) != 2 || ids[0] != userA || ids[1] != userB {
		t.Errorf("newStepUserIds = %v, want the distinct ids [user-a user-b]", ids)
	}
}

// TestTasksReassignDryRunLooksUpUsers: a preview does the user existence read
// (nothing is written) and shows the ids it would send.
func TestTasksReassignDryRunLooksUpUsers(t *testing.T) {
	const taskID = "zz-c1i-test-task-8"
	const user = "aaaaaaaaaaaaaaaaaaaaaaaaaaa"

	var userGets, posts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/users/"):
			userGets = append(userGets, r.URL.Path)
			_, _ = fmt.Fprint(w, `{"id":"user"}`)
		case r.Method == http.MethodGet:
			_, _ = fmt.Fprint(w, reassignTestTaskJSON(taskID, "zz-c1i-test-step"))
		default:
			posts = append(posts, r.URL.Path)
		}
	}))
	defer srv.Close()

	resetCmdFlags(t, tasksReassignCmd)
	_ = tasksReassignCmd.Flags().Set("to-user-id", user)
	stubNewClient(t, srv)
	t.Setenv("C1I_URL", "https://example.invalid")
	withDryRun(t)

	var out bytes.Buffer
	tasksReassignCmd.SetOut(&out)
	t.Cleanup(func() { tasksReassignCmd.SetOut(nil) })
	tasksReassignCmd.SetContext(context.Background())

	if err := tasksReassignCmd.RunE(tasksReassignCmd, []string{taskID}); err != nil {
		t.Fatalf("reassign --dry-run: %v", err)
	}
	if len(userGets) != 1 {
		t.Errorf("--dry-run looked up users: %v, want one lookup", userGets)
	}
	if len(posts) != 0 {
		t.Errorf("--dry-run posted: %v", posts)
	}
	preview := out.String()
	if !strings.HasPrefix(preview, "{\n  \"dryRun\": true") {
		t.Errorf("no preview printed: %q", preview)
	}
	if !strings.Contains(preview, "/action/reassign") || !strings.Contains(preview, user) {
		t.Errorf("preview = %q, want the reassign path and the ids", preview)
	}
}
