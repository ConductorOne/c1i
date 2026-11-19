package cmd

import (
	"strings"
	"testing"
)

func TestAutomationsRefuseDeprecatedSteps(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string // every substring must appear in the error
	}{
		{
			"create --steps-file evaluateExpressions",
			[]string{"automations", "create", "--display-name", "zz",
				"--steps-file", writeTempFile(t, `[{"evaluateExpressions":{}}]`)},
			[]string{"automationSteps[0]", "evaluateExpressions", "no longer accepted"},
		},
		{
			"create --steps-file createRevokeTasks names the replacement",
			[]string{"automations", "create", "--display-name", "zz",
				"--steps-file", writeTempFile(t, `[{"createRevokeTasks":{}}]`)},
			[]string{"automationSteps[0]", "createRevokeTasks", "createRevokeTasksV2"},
		},
		{
			"create --steps-file createRevokeTasks at a later index",
			[]string{"automations", "create", "--display-name", "zz",
				"--steps-file", writeTempFile(t, `[{"waitForDuration":{"duration":"1s"}}, {"createRevokeTasks":{}}]`)},
			[]string{"automationSteps[1]", "createRevokeTasks", "createRevokeTasksV2"},
		},
		{
			"create --body-file camelCase automationSteps",
			[]string{"automations", "create",
				"--body-file", writeTempFile(t, `{"displayName":"zz","automationSteps":[{"evaluateExpressions":{}}]}`)},
			[]string{"automationSteps[0]", "evaluateExpressions"},
		},
		{
			"create --body-file snake_case automation_steps and create_revoke_tasks",
			[]string{"automations", "create",
				"--body-file", writeTempFile(t, `{"displayName":"zz","automation_steps":[{"create_revoke_tasks":{}}]}`)},
			[]string{"automation_steps[0]", "create_revoke_tasks", "createRevokeTasksV2"},
		},
		{
			"create --body-file draftAutomationSteps",
			[]string{"automations", "create",
				"--body-file", writeTempFile(t, `{"displayName":"zz","draftAutomationSteps":[{"evaluateExpressions":{}}]}`)},
			[]string{"draftAutomationSteps[0]", "evaluateExpressions"},
		},
		{
			"update --body-file draft_automation_steps snake_case",
			[]string{"automations", "update", "a-1",
				"--body-file", writeTempFile(t, `{"draft_automation_steps":[{"createRevokeTasks":{}}]}`),
				"--update-mask", "draftAutomationSteps"},
			[]string{"draft_automation_steps[0]", "createRevokeTasks", "createRevokeTasksV2"},
		},
		{
			"update --body-file automationSteps camelCase",
			[]string{"automations", "update", "a-1",
				"--body-file", writeTempFile(t, `{"automationSteps":[{"waitForDuration":{"duration":"1s"}}, {"evaluateExpressions":{}}]}`),
				"--update-mask", "automationSteps"},
			[]string{"automationSteps[1]", "evaluateExpressions"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, fixedReply(200, `{}`))
			r := runMutationCLI(t, s.srv, 0, nil, tc.args...)
			if r.exit != exitUsage {
				t.Fatalf("exit %d (err %v), want %d", r.exit, r.err, exitUsage)
			}
			if r.err == nil {
				t.Fatalf("err is nil, want one naming %v", tc.want)
			}
			for _, want := range tc.want {
				if !strings.Contains(r.err.Error(), want) {
					t.Errorf("err %v, want it to mention %q", r.err, want)
				}
			}
			if got := s.requests(); len(got) != 0 {
				t.Errorf("sent %+v, want nothing", got)
			}
		})
	}
}

// TestAutomationsRefuseDeprecatedStepsDryRun checks the refusal fires before
// --dry-run's own preview, which must never show a request carrying a step
// the server no longer accepts.
func TestAutomationsRefuseDeprecatedStepsDryRun(t *testing.T) {
	withDryRun(t)
	r := runMutationCLI(t, nil, 0, nil, "automations", "create", "--display-name", "zz",
		"--steps-file", writeTempFile(t, `[{"evaluateExpressions":{}}]`))
	if r.exit != exitUsage || r.err == nil || !strings.Contains(r.err.Error(), "evaluateExpressions") {
		t.Fatalf("exit %d err %v, want %d naming evaluateExpressions", r.exit, r.err, exitUsage)
	}
	if strings.Contains(r.stdout, "dryRun") {
		t.Errorf("stdout = %q, want no preview printed", r.stdout)
	}
}

// TestAutomationsRefuseDeprecatedStepsNoHTTPAttempt is the literal
// acceptance check: a steps file containing evaluateExpressions exits 2 with
// no request sent, confirmed with --debug (no "> " trace line, since no
// RoundTrip ever ran -- a client is never even built).
func TestAutomationsRefuseDeprecatedStepsNoHTTPAttempt(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	r := runMutationCLI(t, nil, 0, nil, "automations", "create", "--display-name", "zz",
		"--steps-file", writeTempFile(t, `[{"evaluateExpressions":{}}]`), "--debug")
	if r.exit != exitUsage {
		t.Fatalf("exit %d (err %v), want %d", r.exit, r.err, exitUsage)
	}
	if strings.Contains(r.stderr, "> ") {
		t.Errorf("stderr = %q, want no HTTP trace line", r.stderr)
	}
}

// TestAutomationsValidStepsStillSend is the negative case: a step using a
// supported arm is never refused, and reaches the server.
func TestAutomationsValidStepsStillSend(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, fixedReply(200, `{"automation":{"id":"a-1"}}`))
	r := runMutationCLI(t, s.srv, 0, nil, "automations", "create", "--display-name", "zz",
		"--steps-file", writeTempFile(t, `[{"waitForDuration":{"duration":"1s"}}, {"createRevokeTasksV2":{}}]`))
	if r.err != nil {
		t.Fatalf("exit %d err %v", r.exit, r.err)
	}
	if got := s.requests(); len(got) != 1 {
		t.Errorf("requests = %+v, want exactly one", got)
	}
}
