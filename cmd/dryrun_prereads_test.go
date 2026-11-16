package cmd

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// dryRunCase is a mutation with a pre-send read: ghost names a resource that
// does not exist, so the real run refuses with wantExit.
type dryRunCase struct {
	name     string
	args     []string
	wantExit int
	policy   bool // ghost is a policy id, not a user id
}

func dryRunCases(t *testing.T) []dryRunCase {
	t.Helper()
	steps := writeTempJSON(t, "steps.json", `[{"approval":{}}]`)
	return []dryRunCase{
		{"apps set-owners", []string{"apps", "set-owners", "app1", "--user-id", "ghost"}, exitUsage, false},
		{"tasks reassign", []string{"tasks", "reassign", "t1", "--policy-step-id", "P1", "--to-user-id", "ghost"}, exitUsage, false},
		{"entitlements create owner", []string{"entitlements", "create", "--app-id", "app-1", "--display-name", "e", "--owner-id", "ghost"}, exitUsage, false},
		{"entitlements set-owners", []string{"entitlements", "set-owners", "ent1", "--app-id", "app1", "--user-id", "ghost"}, exitUsage, false},
		{"apps create policy", []string{"apps", "create", "--display-name", "x", "--grant-policy-id", "ghost"}, exitUsage, true},
		{"apps update policy", []string{"apps", "update", "app1", "--grant-policy-id", "ghost"}, exitUsage, true},
		{"entitlements create policy", []string{"entitlements", "create", "--app-id", "app-1", "--display-name", "e", "--grant-policy-id", "ghost"}, exitUsage, true},
		{"entitlements update policy", []string{"entitlements", "update", "ent1", "--app-id", "app1", "--grant-policy-id", "ghost"}, exitUsage, true},
		{"policies update type lookup", []string{"policies", "update", "ghost", "--steps-file", steps}, exitNotFound, true},
	}
}

// preReadServer 404s the id "ghost" on reads, answers other reads as
// existing, and accepts any write.
func preReadServer(t *testing.T) *mutServer {
	t.Helper()
	return newMutServer(t, func(r *http.Request, _ int) mutReply {
		p := r.URL.Path
		if r.Method == http.MethodGet {
			switch {
			case strings.HasSuffix(p, "/ghost"):
				return mutReply{status: http.StatusNotFound, body: `{"code":5,"message":"not found"}`}
			case strings.HasPrefix(p, "/api/v1/users/"):
				return mutReply{body: `{"user":{"id":"u1"}}`}
			case strings.HasPrefix(p, "/api/v1/policies/"):
				return mutReply{body: `{"policy":{"id":"p1","policyType":"POLICY_TYPE_GRANT"}}`}
			}
		}
		return mutReply{body: `{}`}
	})
}

func nonGETRequests(s *mutServer) []entOwnerRequest {
	var out []entOwnerRequest
	for _, rq := range s.requests() {
		if rq.method != http.MethodGet {
			out = append(out, rq)
		}
	}
	return out
}

// TestDryRunRefusesWhatARealRunRefuses: a pre-send existence check runs under
// --dry-run too, so a preview never shows success for a write that would be
// refused, and neither run sends a write.
func TestDryRunRefusesWhatARealRunRefuses(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	for _, tc := range dryRunCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			real := preReadServer(t)
			rr := runMutationCLI(t, real.srv, 0, nil, tc.args...)
			if rr.exit != tc.wantExit {
				t.Fatalf("real run: exit %d (%v), want %d", rr.exit, rr.err, tc.wantExit)
			}

			withDryRun(t)
			dry := preReadServer(t)
			dr := runMutationCLI(t, dry.srv, 0, nil, tc.args...)
			if dr.exit != rr.exit || dr.stderr != rr.stderr {
				t.Errorf("dry run: exit %d stderr %q, want exit %d stderr %q", dr.exit, dr.stderr, rr.exit, rr.stderr)
			}
			if dr.stdout != "" {
				t.Errorf("dry run printed a preview: %q", dr.stdout)
			}
			if w := nonGETRequests(dry); len(w) != 0 {
				t.Errorf("dry run sent %+v", w)
			}
		})
	}
}

// TestDryRunPreviewsAfterPreReadsPass: with the resource present the preview
// prints, after reading, and still sends no write.
func TestDryRunPreviewsAfterPreReadsPass(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	for _, tc := range dryRunCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			existing := "u1"
			if tc.policy {
				existing = "p1"
			}
			args := make([]string, len(tc.args))
			for i, a := range tc.args {
				if a == "ghost" {
					a = existing
				}
				args[i] = a
			}
			withDryRun(t)
			s := preReadServer(t)
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.err != nil {
				t.Fatalf("exit %d: %v", r.exit, r.err)
			}
			if !strings.Contains(r.stdout, `"dryRun": true`) {
				t.Errorf("no preview: %q", r.stdout)
			}
			if len(s.requests()) == 0 {
				t.Error("the pre-send read did not run")
			}
			if w := nonGETRequests(s); len(w) != 0 {
				t.Errorf("dry run sent %+v", w)
			}
		})
	}
}

// TestBadPathIDRefusedBeforePreReads: an empty, "." or ".." id is refused (exit
// 2) before any request, the pre-read GET included, in both modes.
func TestBadPathIDRefusedBeforePreReads(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	for _, id := range []string{".", ".."} {
		cases := map[string][]string{
			"apps set-owners":         {"apps", "set-owners", id, "--user-id", "u1"},
			"entitlements set-owners": {"entitlements", "set-owners", id, "--app-id", "app1", "--user-id", "u1"},
			"tasks reassign":          {"tasks", "reassign", id, "--policy-step-id", "P1", "--to-user-id", "u1"},
			"apps update":             {"apps", "update", id, "--grant-policy-id", "p1"},
			"entitlements create":     {"entitlements", "create", "--app-id", id, "--display-name", "e", "--owner-id", "u1"},
			"entitlements update":     {"entitlements", "update", id, "--app-id", "app1", "--grant-policy-id", "p1"},
		}
		for name, args := range cases {
			for _, dry := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s %q dry=%v", name, id, dry), func(t *testing.T) {
					if dry {
						withDryRun(t)
					}
					s := preReadServer(t)
					r := runMutationCLI(t, s.srv, 0, nil, args...)
					if r.exit != exitUsage {
						t.Errorf("exit %d (%v), want %d", r.exit, r.err, exitUsage)
					}
					if got := s.requests(); len(got) != 0 {
						t.Errorf("requests sent: %+v", got)
					}
				})
			}
		}
	}
}
