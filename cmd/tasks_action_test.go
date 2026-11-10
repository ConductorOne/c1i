package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bytes"
	"context"

	"errors"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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

// taskActionExpectations pins each action's path and step mode.
// TestEveryTaskActionIsPinned requires a row per command, so a new command
// cannot go untested.
var taskActionExpectations = map[string]struct {
	verb string
	step policyStepMode
	// setup supplies flags the command requires before it will run.
	setup func(cmd *cobra.Command)
}{
	"approve":               {verb: "approve", step: stepRequired},
	"deny":                  {verb: "deny", step: stepOptional},
	"close":                 {verb: "close", step: stepUnused},
	"restart":               {verb: "restart", step: stepOptional},
	"reset":                 {verb: "reset", step: stepUnused},
	"skip-step":             {verb: "skip-step", step: stepRequired},
	"process":               {verb: "process", step: stepUnused},
	"comment":               {verb: "comment", step: stepUnused, setup: func(c *cobra.Command) { _ = c.Flags().Set("comment", "zz") }},
	"update-grant-duration": {verb: "update-grant-duration", step: stepUnused, setup: func(c *cobra.Command) { _ = c.Flags().Set("duration", "3600s") }},
	"reassign":              {verb: "reassign", step: stepRequired, setup: func(c *cobra.Command) { _ = c.Flags().Set("to-user-id", "zz-user") }},
}

// nonActionTaskSubcommands are the tasks subcommands that are not action POSTs.
var nonActionTaskSubcommands = map[string]bool{"list": true, "get": true}

// TestEveryTaskActionIsPinned requires a row for every action in the tree.
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

// TestEveryTaskActionPostsItsOwnVerbAndStep checks path and policyStepId on
// the wire. A copied verb performs a different action while printing success.
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
			if want.step != stepUnused {
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

// TestTasksCommentAlwaysSendsTheCommentKey: an omitted key records nothing
// while the command still prints success.
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

// TestTasksDenyOmitsAnUnresolvableStep: the field must be absent, not empty,
// and the denial must still go through.
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

// TestTaskActionsPrintTheUnwrappedTask pins the mutation contract for every
// action: stdout is exactly the response's task, unwrapped like "tasks get",
// with no synthesized text. The endpoints return the task as it was BEFORE the
// action, so .state is the server's snapshot, passed through untouched.
func TestTaskActionsPrintTheUnwrappedTask(t *testing.T) {
	for _, name := range []string{"approve", "deny", "restart", "reset", "skip-step", "process", "close", "comment", "update-grant-duration", "reassign"} {
		t.Run(name, func(t *testing.T) {
			cmd := findTasksSubcommand(t, name)
			resetCmds(t, cmd)
			if name == "comment" {
				_ = cmd.Flags().Set("comment", "zz")
			}
			if name == "update-grant-duration" {
				_ = cmd.Flags().Set("duration", "3600s")
			}
			if name == "reassign" {
				_ = cmd.Flags().Set("to-user-id", "zz-user")
			}
			r := newTaskActionRecorder(t, "TASK_STATE_OPEN", "zz-step-1111111111111111111")
			out, err := runTaskActionCmd(t, cmd, r.srv, actionTestTaskID)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			want := "{\n  \"id\": \"" + actionTestTaskID + "\",\n  \"state\": \"TASK_STATE_OPEN\",\n  \"expanded\": []\n}\n"
			if out != want {
				t.Errorf("%s stdout = %q, want %q", name, out, want)
			}
		})
	}
}

// TestTasksRestartOmitsEmptyPolicyStepField pins that a closed task, which has
// no current step, sends no policyStepId and prints only the task.
func TestTasksRestartOmitsEmptyPolicyStepField(t *testing.T) {
	cmd := findTasksSubcommand(t, "restart")
	resetCmds(t, cmd)
	r := newTaskActionRecorder(t, "TASK_STATE_CLOSED", "")
	out, err := runTaskActionCmd(t, cmd, r.srv, actionTestTaskID)
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	if strings.Contains(out, "policy_step_id") || strings.Contains(out, "policyStepId") {
		t.Errorf("restart printed a policy step field: %q", out)
	}
	if got, ok := r.bodies[0]["policyStepId"]; ok {
		t.Errorf("restart sent policyStepId=%v with no current step", got)
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

// TestEveryTaskActionModeBehavesOnAnUnresolvableStep separates stepRequired
// from stepOptional: with no derivable step, required errors before sending,
// optional sends without the field.
func TestEveryTaskActionModeBehavesOnAnUnresolvableStep(t *testing.T) {
	for name, want := range taskActionExpectations {
		if want.step == stepUnused {
			continue
		}
		t.Run(name, func(t *testing.T) {
			cmd := findTasksSubcommand(t, name)
			resetCmds(t, cmd)
			if want.setup != nil {
				want.setup(cmd)
			}
			r := newTaskActionRecorder(t, "TASK_STATE_OPEN", "") // no current step
			_, err := runTaskActionCmd(t, cmd, r.srv, actionTestTaskID)
			if want.step == stepRequired {
				if err == nil {
					t.Fatalf("%s is stepRequired but succeeded with no derivable step", name)
				}
				if got := exitCode(err); got != exitUsage {
					t.Errorf("%s exitCode = %d, want %d (exitUsage); err = %v", name, got, exitUsage, err)
				}
				if len(r.paths) != 0 {
					t.Errorf("%s sent a request despite requiring a step: %v", name, r.paths)
				}
				return
			}
			// stepOptional: proceed, with the field omitted.
			if err != nil {
				t.Fatalf("%s is stepOptional but failed with no derivable step: %v", name, err)
			}
			if _, ok := r.bodies[0]["policyStepId"]; ok {
				t.Errorf("%s sent policyStepId when none could be resolved", name)
			}
		})
	}
}

// TestTaskActionsDryRunNeverSends: --dry-run must never reach the wire.
func TestTaskActionsDryRunNeverSends(t *testing.T) {
	for name, want := range taskActionExpectations {
		t.Run(name, func(t *testing.T) {
			cmd := findTasksSubcommand(t, name)
			resetCmds(t, cmd)
			if want.setup != nil {
				want.setup(cmd)
			}
			var posted []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if req.Method == http.MethodPost {
					posted = append(posted, req.URL.Path)
					t.Errorf("--dry-run sent a real POST to %s", req.URL.Path)
				}
				// The GET is the policy-step lookup, which a preview may make.
				_, _ = w.Write([]byte(`{"taskView":{"task":{"id":"` + actionTestTaskID +
					`","state":"TASK_STATE_OPEN","policy":{"current":{"id":"zz-step-1111111111111111111"}}}}}`))
			}))
			t.Cleanup(srv.Close)

			stubNewClient(t, srv)
			t.Setenv("C1I_URL", "https://example.invalid")
			withDryRun(t)

			var out bytes.Buffer
			cmd.SetOut(&out)
			// Package-level singleton: a left-attached buffer swallows this
			// command's output in every later test.
			t.Cleanup(func() { cmd.SetOut(nil) })
			cmd.SetContext(context.Background())
			if err := cmd.RunE(cmd, []string{actionTestTaskID}); err != nil {
				t.Fatalf("%s --dry-run: %v", name, err)
			}
			if len(posted) != 0 {
				t.Errorf("%s posted during a dry run: %v", name, posted)
			}
			if !strings.HasPrefix(out.String(), "{\n  \"dryRun\": true") {
				t.Errorf("%s printed no preview: %q", name, out.String())
			}
			if !strings.Contains(out.String(), "/action/"+want.verb) {
				t.Errorf("%s previewed the wrong path: %q", name, out.String())
			}
		})
	}
}

// TestTasksUpdateGrantDurationSendsDurationKey. "grantDuration" is the
// plausible wrong name: it is what the response carries.
func TestTasksUpdateGrantDurationSendsDurationKey(t *testing.T) {
	cmd := findTasksSubcommand(t, "update-grant-duration")
	resetCmds(t, cmd)
	_ = cmd.Flags().Set("duration", "3600s")
	r := newTaskActionRecorder(t, "TASK_STATE_OPEN", "zz-step-1111111111111111111")
	if _, err := runTaskActionCmd(t, cmd, r.srv, actionTestTaskID); err != nil {
		t.Fatalf("update-grant-duration: %v", err)
	}
	if got, ok := r.bodies[0]["duration"]; !ok || got != "3600s" {
		t.Errorf(`body["duration"] = %v (present=%v), want "3600s"`, got, ok)
	}
	if _, ok := r.bodies[0]["grantDuration"]; ok {
		t.Error(`body carries "grantDuration"; that is the response field, not the request's`)
	}
}

// TestTasksUpdateGrantDurationConvertsGoForm: the server 400s "1h", so a Go
// duration is converted to seconds. Zero is the server's call; a negative or
// decimal value never sends.
func TestTasksUpdateGrantDurationConvertsGoForm(t *testing.T) {
	cmd := findTasksSubcommand(t, "update-grant-duration")
	for in, want := range map[string]string{"8h": "28800s", " 90m ": "5400s", "3600s": "3600s", "0s": "0s"} {
		resetCmds(t, cmd)
		_ = cmd.Flags().Set("duration", in)
		r := newTaskActionRecorder(t, "TASK_STATE_OPEN", "zz-step-1111111111111111111")
		if _, err := runTaskActionCmd(t, cmd, r.srv, actionTestTaskID); err != nil {
			t.Fatalf("--duration %q: %v", in, err)
		}
		if got := r.bodies[0]["duration"]; got != want {
			t.Errorf(`--duration %q: body["duration"] = %v, want %q`, in, got, want)
		}
	}
	for _, in := range []string{"-1h", "1.5h", "7d"} {
		resetCmds(t, cmd)
		_ = cmd.Flags().Set("duration", in)
		r := newTaskActionRecorder(t, "TASK_STATE_OPEN", "zz-step-1111111111111111111")
		_, err := runTaskActionCmd(t, cmd, r.srv, actionTestTaskID)
		if got := exitCode(err); got != exitUsage {
			t.Errorf("--duration %q: exit = %d, want %d (err: %v)", in, got, exitUsage, err)
		}
		if len(r.paths) != 0 {
			t.Errorf("--duration %q: a request was sent: %v", in, r.paths)
		}
	}
}

// TestTaskActionsDryRunStillResolvesTheURL: a bad --url must fail even under
// --dry-run. The other dry-run tests pass a valid URL, so only this sees it.
func TestTaskActionsDryRunStillResolvesTheURL(t *testing.T) {
	// Both modes: the runner resolves the URL once, before either path.
	for _, name := range []string{"close", "restart"} {
		t.Run(name, func(t *testing.T) {
			resetCmdFlags(t, rootCmd)
			t.Setenv("C1I_URL", "")
			withDryRun(t)

			var out bytes.Buffer
			rootCmd.SetOut(&out)
			rootCmd.SetErr(&out)
			rootCmd.SetArgs([]string{"tasks", name, actionTestTaskID, "--dry-run", "--url", "not a url"})
			err := rootCmd.ExecuteContext(t.Context())
			if err == nil {
				t.Fatalf("tasks %s --dry-run accepted a malformed --url; a typo'd tenant previews as though it were real", name)
			}
			if got := exitCode(err); got != exitUsage {
				t.Errorf("exitCode = %d, want %d (exitUsage); err = %v", got, exitUsage, err)
			}
			if strings.Contains(out.String(), "dryRun") {
				t.Errorf("tasks %s printed a preview for an unresolvable URL: %q", name, out.String())
			}
		})
	}
}

// withDryRun turns dry-run on for one test. Cleanup restores viper's "no
// override" state rather than the previous value: an override outranks both a
// bound env var and a changed pflag, so parking one here would mask
// C1I_DRY_RUN in whichever test runs next.
func withDryRun(t *testing.T) {
	t.Helper()
	viper.Set("dry_run", true)
	t.Cleanup(func() { viper.Set("dry_run", nil) })
}

// TestStepUnusedActionsPreviewWithoutCredentials: an action needing no step
// previews without authenticating; one needing a step must authenticate.
func TestStepUnusedActionsPreviewWithoutCredentials(t *testing.T) {
	for name, want := range taskActionExpectations {
		t.Run(name, func(t *testing.T) {
			cmd := findTasksSubcommand(t, name)
			resetCmds(t, cmd)
			if want.setup != nil {
				want.setup(cmd)
			}
			// A client that always fails, standing in for absent credentials.
			orig := newClient
			newClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
				return nil, errNoCredentialsForTest
			}
			t.Cleanup(func() { newClient = orig })

			t.Setenv("C1I_URL", "https://example.invalid")
			withDryRun(t)

			var out bytes.Buffer
			cmd.SetOut(&out)
			t.Cleanup(func() { cmd.SetOut(nil) })
			cmd.SetContext(context.Background())
			err := cmd.RunE(cmd, []string{actionTestTaskID})

			if want.step == stepUnused {
				if err != nil {
					t.Fatalf("%s --dry-run needs credentials it should not: %v", name, err)
				}
				if !strings.HasPrefix(out.String(), "{\n  \"dryRun\": true") {
					t.Errorf("%s printed no preview: %q", name, out.String())
				}
				return
			}
			// Step-using actions must fetch the step, so they authenticate
			// first even for a preview — as they did before sharing a runner.
			if err == nil {
				t.Fatalf("%s --dry-run should have failed without credentials; it resolves a policy step", name)
			}
		})
	}
}

// errNoCredentialsForTest stands in for a credential-loading failure.
var errNoCredentialsForTest = errors.New("authentication failed: no credentials found")
