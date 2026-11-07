package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The access-profiles, tasks and requests create families go through
// sendMutation; these tests drive them end to end through rootCmd.

const (
	accessTaskEnvelope = `{"taskView":{"task":{"id":"task-1","state":"TASK_STATE_OPEN"}},"ticketActionId":"act-1","expanded":[]}`
	accessTaskDoc      = "{\n  \"id\": \"task-1\",\n  \"state\": \"TASK_STATE_OPEN\",\n  \"expanded\": [],\n  \"ticketActionId\": \"act-1\"\n}\n"
	accessStepGet      = `{"taskView":{"task":{"id":"task-1","policy":{"current":{"id":"step-9"}}}}}`
)

// accessRoute answers GETs with the policy-step and introspect lookups and
// every write with body.
func accessRoute(body string) func(*http.Request, int) mutReply {
	return func(r *http.Request, _ int) mutReply {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/auth/introspect":
			return mutReply{body: `{"userId":"self-1"}`}
		case r.Method == http.MethodGet:
			return mutReply{body: accessStepGet}
		}
		return mutReply{body: body}
	}
}

// R8a: a step c1i looked up goes to stderr, never into the document.
func TestTasksResolvedStepIsReportedOnStderrOnly(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, accessRoute(accessTaskEnvelope))
	r := runMutationCLI(t, s.srv, 0, nil, "tasks", "approve", "task-1")
	if r.err != nil || r.stdout != accessTaskDoc {
		t.Fatalf("exit %d stdout %q err %v", r.exit, r.stdout, r.err)
	}
	if want := "Resolved policy step: step-9\n"; r.stderr != want {
		t.Errorf("stderr = %q, want %q", r.stderr, want)
	}
	if reqs := s.requests(); len(reqs) != 2 || !strings.Contains(reqs[1].body, `"policyStepId":"step-9"`) {
		t.Errorf("requests = %+v, want the lookup then a POST carrying step-9", reqs)
	}

	t.Run("json", func(t *testing.T) {
		s := newMutServer(t, accessRoute(accessTaskEnvelope))
		r := runMutationCLI(t, s.srv, 0, nil, "tasks", "approve", "task-1", "--error-format", "json")
		want := `{"id":"step-9","kind":"resolved","name":"policyStepId","notice":"Resolved policy step: step-9"}` + "\n"
		if r.stderr != want {
			t.Errorf("stderr = %q, want %q", r.stderr, want)
		}
	})
	t.Run("explicit step is not reported", func(t *testing.T) {
		s := newMutServer(t, accessRoute(accessTaskEnvelope))
		r := runMutationCLI(t, s.srv, 0, nil, "tasks", "approve", "task-1", "--policy-step-id", "mine")
		if r.stderr != "" || len(s.requests()) != 1 {
			t.Errorf("stderr %q, %d requests; want nothing reported and no lookup", r.stderr, len(s.requests()))
		}
	})
}

func TestAccessMutationPreview(t *testing.T) {
	preview := wantPreview
	t.Run("profiles delete", func(t *testing.T) {
		withDryRun(t)
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, nil, 0, nil, "access-profiles", "delete", "cat/1")
		if want := preview("DELETE", "/api/v1/catalogs/cat%2F1", ""); r.err != nil || r.stdout != want {
			t.Errorf("exit %d stdout %q, want %q", r.exit, r.stdout, want)
		}
		if r.stderr != "dry run: nothing was sent\n" {
			t.Errorf("stderr = %q", r.stderr)
		}
	})
	t.Run("tasks approve resolves the step", func(t *testing.T) {
		withDryRun(t)
		t.Setenv("C1I_URL", mutTestURL)
		s := newMutServer(t, accessRoute(accessTaskEnvelope))
		r := runMutationCLI(t, s.srv, 0, nil, "tasks", "approve", "task-1")
		want := preview("POST", "/api/v1/tasks/task-1/action/approve", "{\n        \"policyStepId\": \"step-9\"\n      }")
		if r.err != nil || r.stdout != want {
			t.Errorf("exit %d stdout %q, want %q", r.exit, r.stdout, want)
		}
		if reqs := s.requests(); len(reqs) != 1 || reqs[0].method != http.MethodGet {
			t.Errorf("requests = %+v, want only the step lookup", reqs)
		}
	})
	t.Run("grant with --user-id needs no credentials", func(t *testing.T) {
		withDryRun(t)
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, nil, 0, nil, "requests", "create", "grant", "--app-id", "a", "--entitlement-id", "e", "--user-id", "u1")
		want := preview("POST", "/api/v1/task/grant", "{\n        \"appEntitlementId\": \"e\",\n        \"appId\": \"a\",\n        \"identityUserId\": \"u1\"\n      }")
		if r.err != nil || r.stdout != want {
			t.Errorf("exit %d stdout %q, want %q", r.exit, r.stdout, want)
		}
	})
	t.Run("revoke without --user-id resolves self", func(t *testing.T) {
		withDryRun(t)
		t.Setenv("C1I_URL", mutTestURL)
		s := newMutServer(t, accessRoute(accessTaskEnvelope))
		r := runMutationCLI(t, s.srv, 0, nil, "requests", "create", "revoke", "--app-id", "a", "--entitlement-id", "e")
		var doc struct {
			Requests []struct {
				Body struct{ IdentityUserID string }
			}
		}
		if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil || len(doc.Requests) != 1 || doc.Requests[0].Body.IdentityUserID != "self-1" {
			t.Errorf("stdout = %q (%v), want identityUserId self-1", r.stdout, err)
		}
	})
}

func TestAccessMutationApplied(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		route   func(*http.Request, int) mutReply
		exit    int
		applied any
	}{
		{"profiles delete 404", []string{"access-profiles", "delete", "cat-1"}, fixedReply(404, `{}`), exitNotFound, false},
		{"profiles create 500", []string{"access-profiles", "create", "--display-name", "zz"}, fixedReply(500, ``), exitServer, "unknown"},
		{"bundle run 404", []string{"access-profiles", "bundle-automation", "run", "cat-1"}, fixedReply(404, `{}`), exitNotFound, false},
		{"visibility add 404", []string{"access-profiles", "visibility-entitlements", "add", "cat-1", "--app-id", "a", "--entitlement-id", "e"}, fixedReply(404, `{}`), exitNotFound, false},
		{"tasks close 404", []string{"tasks", "close", "task-1"}, fixedReply(404, `{}`), exitNotFound, false},
		{"tasks close non-JSON", []string{"tasks", "close", "task-1"}, fixedReply(200, `<html>`), exitServer, true},
		{"tasks approve lookup 404", []string{"tasks", "approve", "task-1"}, fixedReply(404, `{}`), exitNotFound, false},
		{"requests grant 404", []string{"requests", "create", "grant", "--app-id", "a", "--entitlement-id", "e", "--user-id", "u"}, fixedReply(404, `{}`), exitNotFound, false},
		{"requests revoke 429", []string{"requests", "create", "revoke", "--app-id", "a", "--entitlement-id", "e", "--user-id", "u"}, fixedReply(429, ``), exitRateLimited, false},
		{"requests grant self lookup 404", []string{"requests", "create", "grant", "--app-id", "a", "--entitlement-id", "e"}, fixedReply(404, `{}`), exitNotFound, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newMutServer(t, tc.route)
			t.Setenv("C1I_URL", mutTestURL)
			r := runMutationCLI(t, s.srv, 0, nil, append(tc.args, "--error-format", "json")...)
			if r.exit != tc.exit {
				t.Errorf("exit = %d, want %d (%v)", r.exit, tc.exit, r.err)
			}
			if got := r.errorObject(t)["applied"]; got != tc.applied {
				t.Errorf("applied = %#v, want %#v; stderr %s", got, tc.applied, r.stderr)
			}
			if r.stdout != "" {
				t.Errorf("stdout = %q, want nothing", r.stdout)
			}
		})
	}
}

// A failed stdout write names the request and exits 1.
func TestAccessMutationWriteFailure(t *testing.T) {
	s := newMutServer(t, fixedReply(200, accessTaskEnvelope))
	t.Setenv("C1I_URL", mutTestURL)
	r := runMutationCLI(t, s.srv, 0, failingWriter{}, "tasks", "close", "task-1", "--error-format", "json")
	if r.exit != exitError || r.errorObject(t)["applied"] != true {
		t.Errorf("exit %d, stderr %s; want 1 and applied:true", r.exit, r.stderr)
	}
}
