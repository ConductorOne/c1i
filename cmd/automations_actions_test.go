package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func fastExecutionWait(t *testing.T) {
	t.Helper()
	orig := executionWaitInterval
	executionWaitInterval = 5 * time.Millisecond
	t.Cleanup(func() { executionWaitInterval = orig })
}

// executionRoute answers the execute POST with execution 7 and each GET with
// the next of states (the last one repeats).
func executionRoute(states ...string) func(*http.Request, int) mutReply {
	gets := 0
	return func(r *http.Request, _ int) mutReply {
		if r.Method == http.MethodPost {
			return mutReply{body: `{"executionId":"7"}`}
		}
		s := states[min(gets, len(states)-1)]
		gets++
		return mutReply{body: `{"automationExecution":{"id":"7","state":"` + s + `"},"expanded":[],"view":null}`}
	}
}

func TestAutomationsExecuteWait(t *testing.T) {
	fastExecutionWait(t)
	t.Setenv("C1I_URL", mutTestURL)

	t.Run("polls until DONE and prints the id first", func(t *testing.T) {
		s := newMutServer(t, executionRoute("AUTOMATION_EXECUTION_STATE_PENDING", "AUTOMATION_EXECUTION_STATE_WAITING", executionStateDone))
		r := runMutationCLI(t, s.srv, 0, nil, "automations", "execute", "a-1", "--wait")
		if r.err != nil || !strings.Contains(r.stdout, `"executionId": "7"`) {
			t.Fatalf("exit %d err %v stdout %q", r.exit, r.err, r.stdout)
		}
		if got := s.requests(); len(got) != 4 || got[1].rawPath != "/api/v1/automation_executions/7" {
			t.Errorf("requests = %+v, want the POST then three GETs of execution 7", got)
		}
		if !strings.Contains(r.stderr, "Execution 7 finished") {
			t.Errorf("stderr = %q, want the success line", r.stderr)
		}
	})
	for _, state := range []string{executionStateError, executionStateTerm, executionStatePaused} {
		t.Run("ends in "+state, func(t *testing.T) {
			s := newMutServer(t, executionRoute("AUTOMATION_EXECUTION_STATE_PROCESS_STEP", state))
			r := runMutationCLI(t, s.srv, 0, nil, "automations", "execute", "a-1", "--wait", "--error-format", "json")
			if r.exit != exitError || !strings.Contains(r.stdout, `"7"`) {
				t.Fatalf("exit %d stdout %q, want 1 and the executionId", r.exit, r.stdout)
			}
			obj := r.errorObject(t)
			msg, _ := obj["error"].(string)
			if obj["applied"] != true || !strings.Contains(msg, "execution 7 was started") || !strings.Contains(msg, state) {
				t.Errorf("error object = %v", obj)
			}
		})
	}
	t.Run("timeout exits 1, applied", func(t *testing.T) {
		s := newMutServer(t, executionRoute("AUTOMATION_EXECUTION_STATE_WAITING"))
		r := runMutationCLI(t, s.srv, 0, nil, "automations", "execute", "a-1", "--wait", "--wait-timeout", "50ms", "--error-format", "json")
		if r.exit != exitError || r.errorObject(t)["applied"] != true {
			t.Errorf("exit %d stderr %s, want 1 and applied:true", r.exit, r.stderr)
		}
	})
	t.Run("an execution read without a state exits 6", func(t *testing.T) {
		s := newMutServer(t, func(r *http.Request, _ int) mutReply {
			if r.Method == http.MethodPost {
				return mutReply{body: `{"executionId":"7"}`}
			}
			return mutReply{body: `{"automationExecution":{"id":"7"}}`}
		})
		r := runMutationCLI(t, s.srv, 0, nil, "automations", "execute", "a-1", "--wait", "--error-format", "json")
		if r.exit != exitServer || r.errorObject(t)["applied"] != true {
			t.Errorf("exit %d stderr %s, want 6 and applied:true", r.exit, r.stderr)
		}
	})
	t.Run("response without executionId exits 6 and polls nothing", func(t *testing.T) {
		s := newMutServer(t, fixedReply(200, `{}`))
		r := runMutationCLI(t, s.srv, 0, nil, "automations", "execute", "a-1", "--wait", "--error-format", "json")
		if r.exit != exitServer || r.errorObject(t)["applied"] != true {
			t.Errorf("exit %d stderr %s, want 6 and applied:true", r.exit, r.stderr)
		}
		if got := s.requests(); len(got) != 1 {
			t.Errorf("requests = %+v, want only the POST", got)
		}
	})
	t.Run("without --wait nothing is polled", func(t *testing.T) {
		s := newMutServer(t, executionRoute(executionStateError))
		r := runMutationCLI(t, s.srv, 0, nil, "automations", "execute", "a-1")
		if r.err != nil || len(s.requests()) != 1 {
			t.Errorf("exit %d requests %+v, want 0 and only the POST", r.exit, s.requests())
		}
	})
	t.Run("dry run never polls", func(t *testing.T) {
		withDryRun(t)
		r := runMutationCLI(t, nil, 0, nil, "automations", "execute", "a-1", "--wait")
		if r.err != nil || !strings.Contains(r.stdout, `"dryRun": true`) {
			t.Errorf("exit %d err %v stdout %q", r.exit, r.err, r.stdout)
		}
	})
}

// sentBody runs args and returns the one write's body, decoded.
func sentBody(t *testing.T, reply string, args ...string) map[string]any {
	t.Helper()
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, fixedReply(200, reply))
	r := runMutationCLI(t, s.srv, 0, nil, args...)
	if r.err != nil {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
	got := s.requests()
	if len(got) != 1 {
		t.Fatalf("requests = %+v, want one write", got)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(got[0].body), &body); err != nil {
		t.Fatalf("body %q: %v", got[0].body, err)
	}
	return body
}

func TestAutomationsActionBodies(t *testing.T) {
	const created = `{"automation":{"id":"a-1"}}`
	cases := []struct {
		name  string
		reply string
		args  []string
		want  string
	}{
		{"create from flags", created, []string{"automations", "create", "--display-name", "zz",
			"--steps-file", writeTempFile(t, `[{"waitForDuration":{"duration":"1s"}}]`), "--triggers-file", writeTempFile(t, `[]`)},
			`{"automationSteps":[{"waitForDuration":{"duration":"1s"}}],"displayName":"zz","triggers":[]}`},
		{"create sends an explicit --enabled=false", created, []string{"automations", "create", "--display-name", "zz", "--enabled=false"},
			`{"displayName":"zz","enabled":false}`},
		{"create from --body-file verbatim", created, []string{"automations", "create", "--body-file", writeTempFile(t, `{"displayName":"zz","circuitBreakerMax":3}`)},
			`{"circuitBreakerMax":3,"displayName":"zz"}`},
		{"execute without context", `{"executionId":"7"}`, []string{"automations", "execute", "a-1"}, `{}`},
		{"execute with context", `{"executionId":"7"}`, []string{"automations", "execute", "a-1", "--context-file", writeTempFile(t, `{"k":1}`)},
			`{"context":{"context":{"k":1}}}`},
		{"clear without a decision", `{}`, []string{"automations", "circuit-breaker", "clear", "a-1"}, `{}`},
		{"clear with the API enum spelling", `{}`, []string{"automations", "circuit-breaker", "clear", "a-1", "--decision", "PAUSED_EXECUTION_DECISION_CANCEL", "--reason", "r"},
			`{"decision":"PAUSED_EXECUTION_DECISION_CANCEL","reason":"r"}`},
		{"resolve-paused run", `{}`, []string{"automations", "circuit-breaker", "resolve-paused", "a-1", "--decision", "run"},
			`{"decision":"PAUSED_EXECUTION_DECISION_RUN"}`},
		{"cancel-pending without reason", `{}`, []string{"automations", "cancel-pending", "a-1"}, `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := json.Marshal(sentBody(t, tc.reply, tc.args...))
			if string(b) != tc.want {
				t.Errorf("body = %s, want %s", b, tc.want)
			}
		})
	}
}

func TestAutomationsActionUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"create without a name", []string{"automations", "create"}, "--display-name is required"},
		{"create mixes --body-file with a field flag", []string{"automations", "create", "--body-file", writeTempFile(t, `{}`), "--enabled"}, "mutually exclusive with --enabled"},
		{"create reads stdin twice", []string{"automations", "create", "--display-name", "zz", "--steps-file", "-", "--triggers-file", "-"}, "only one of"},
		{"create --steps-file not an array", []string{"automations", "create", "--display-name", "zz", "--steps-file", writeTempFile(t, `{}`)}, "--steps-file"},
		{"execute --context-file not an object", []string{"automations", "execute", "a-1", "--context-file", writeTempFile(t, `[]`)}, "--context-file"},
		{"resolve-paused without --decision", []string{"automations", "circuit-breaker", "resolve-paused", "a-1"}, "decision"},
		{"unknown --decision", []string{"automations", "circuit-breaker", "clear", "a-1", "--decision", "later"}, "invalid --decision"},
		{"unspecified is not a decision", []string{"automations", "circuit-breaker", "resolve-paused", "a-1", "--decision", "PAUSED_EXECUTION_DECISION_UNSPECIFIED"}, "invalid --decision"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, fixedReply(200, `{}`))
			r := runMutationCLI(t, s.srv, 0, nil, tc.args...)
			if r.exit != exitUsage || r.err == nil || !strings.Contains(r.err.Error(), tc.want) {
				t.Errorf("exit %d err %v, want 2 mentioning %q", r.exit, r.err, tc.want)
			}
			if got := s.requests(); len(got) != 0 {
				t.Errorf("sent %+v, want nothing", got)
			}
		})
	}
}
