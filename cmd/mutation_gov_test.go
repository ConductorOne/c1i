package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// create-task keeps its envelope whatever the finding's own taskId holds:
// the ids are .taskId and .finding.id.
func TestFindingsCreateTaskKeepsIdentity(t *testing.T) {
	for _, inner := range []string{`""`, `"t0"`} {
		t.Run("finding.taskId="+inner, func(t *testing.T) {
			body := `{"finding":{"id":"f1","taskId":` + inner + `},"taskId":"t1"}`
			s := newMutServer(t, fixedReply(200, body))
			t.Setenv("C1I_URL", mutTestURL)
			r := runMutationCLI(t, s.srv, 0, nil, "findings", "create-task", "f1")
			if want := indentJSON(t, body); r.err != nil || r.stdout != want {
				t.Fatalf("exit %d stdout %q, want %q", r.exit, r.stdout, want)
			}
			var doc struct {
				TaskID  string `json:"taskId"`
				Finding struct {
					ID string `json:"id"`
				} `json:"finding"`
			}
			if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil || doc.TaskID != "t1" || doc.Finding.ID != "f1" {
				t.Errorf("stdout %q: want .taskId t1 and .finding.id f1 (err %v)", r.stdout, err)
			}
		})
	}
}

func TestGovMutationPreview(t *testing.T) {
	withDryRun(t)
	t.Setenv("C1I_URL", mutTestURL)
	r := runMutationCLI(t, nil, 0, nil, "findings", "routing-rules", "delete", "r/1")
	want := wantPreview("DELETE", "/api/v1/findings/routing-rules/r%2F1", "")
	if r.err != nil || r.stdout != want {
		t.Errorf("exit %d stdout %q, want %q", r.exit, r.stdout, want)
	}
	if r.stderr != "dry run: nothing was sent\n" {
		t.Errorf("stderr = %q", r.stderr)
	}
}

// A policies update whose patch needs the policy's type looks it up inside
// the helper; the lookup failing sends nothing and reports applied:false.
func TestPoliciesUpdateTypeLookup(t *testing.T) {
	stepsFile := func(t *testing.T) string {
		return writeTempFile(t, `[{"approval":{"users":{"userIds":["u1"]}}},{"accept":{}}]`)
	}
	t.Run("lookup then send", func(t *testing.T) {
		s := newMutServer(t, func(r *http.Request, _ int) mutReply {
			if r.Method == http.MethodGet {
				return mutReply{body: `{"policy":{"id":"pol1","policyType":"POLICY_TYPE_GRANT"}}`}
			}
			return mutReply{body: `{"policy":{"id":"pol1"}}`}
		})
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, s.srv, 0, nil, "policies", "update", "pol1", "--steps-file", stepsFile(t))
		reqs := s.requests()
		if r.err != nil || len(reqs) != 2 || reqs[0].method != http.MethodGet || reqs[1].method != http.MethodPost {
			t.Fatalf("exit %d err %v requests %v", r.exit, r.err, reqs)
		}
		if !strings.Contains(reqs[1].body, `"grant"`) {
			t.Errorf("body %s lacks the grant baseline key", reqs[1].body)
		}
	})
	t.Run("lookup 404", func(t *testing.T) {
		s := newMutServer(t, fixedReply(404, `{}`))
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, s.srv, 0, nil, "policies", "update", "pol1", "--steps-file", stepsFile(t), "--error-format", "json")
		if r.exit != exitNotFound || len(s.requests()) != 1 || r.errorObject(t)["applied"] != false {
			t.Errorf("exit %d, %d requests, stderr %q", r.exit, len(s.requests()), r.stderr)
		}
	})
	t.Run("dry run looks up and previews the full body", func(t *testing.T) {
		withDryRun(t)
		s := newMutServer(t, fixedReply(200, `{"policy":{"id":"pol1","policyType":"POLICY_TYPE_GRANT"}}`))
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, s.srv, 0, nil, "policies", "update", "pol1", "--steps-file", stepsFile(t))
		reqs := s.requests()
		if r.err != nil || len(reqs) != 1 || reqs[0].method != http.MethodGet || !strings.Contains(r.stdout, `"grant"`) {
			t.Errorf("exit %d err %v requests %v stdout %q, want one GET and a preview with the grant key", r.exit, r.err, reqs, r.stdout)
		}
	})
	t.Run("dry run lookup 404 refuses like a real run", func(t *testing.T) {
		withDryRun(t)
		s := newMutServer(t, fixedReply(404, `{}`))
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, s.srv, 0, nil, "policies", "update", "pol1", "--steps-file", stepsFile(t))
		if r.exit != exitNotFound || r.stdout != "" {
			t.Errorf("exit %d stdout %q, want exit 4 and nothing", r.exit, r.stdout)
		}
	})
}
