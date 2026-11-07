package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// govCase is one governance mutation with the response it is shown and the
// document it must print.
type govCase struct {
	name string
	args []string // "@name" stands for a temp JSON file
	body string
	want string // compact JSON; "" means the body is printed as-is
}

func govCases() []govCase {
	return []govCase{
		{"findings create", []string{"findings", "create", "--body-file", "@body"}, `{"finding":{"id":"f1","state":"OPEN"}}`, `{"id":"f1","state":"OPEN"}`},
		{"findings state", []string{"findings", "state", "f1", "--body-file", "@body"}, `{"finding":{"id":"f1","state":"CLOSED"}}`, `{"id":"f1","state":"CLOSED"}`},
		{"findings assignee", []string{"findings", "assignee", "f1", "--body-file", "@body"}, `{"finding":{"id":"f1","assigneeUserId":"u1"}}`, `{"id":"f1","assigneeUserId":"u1"}`},
		{"findings create-task", []string{"findings", "create-task", "f1"}, `{"finding":{"id":"f1"},"taskId":"t1"}`, ""},
		{"findings bulk-state", []string{"findings", "bulk-state", "--body-file", "@body"}, `{"bulkActionId":"b1"}`, ""},
		{"findings bulk-create-tasks", []string{"findings", "bulk-create-tasks", "--body-file", "@body"}, `{"bulkActionId":"b1"}`, ""},
		{"findings settings update", []string{"findings", "settings", "update", "--body-file", "@body"}, `{"list":[{"id":"s1"}]}`, ""},
		{"findings routing-rules create", []string{"findings", "routing-rules", "create", "--body-file", "@body"}, `{"routingRule":{"id":"r1"}}`, `{"id":"r1"}`},
		{"findings routing-rules update", []string{"findings", "routing-rules", "update", "r1", "--body-file", "@body"}, `{"routingRule":{"id":"r1","displayName":"n"}}`, `{"id":"r1","displayName":"n"}`},
		{"findings routing-rules delete", []string{"findings", "routing-rules", "delete", "r1"}, `{}`, `{}`},
		{"findings transformation-rules create", []string{"findings", "transformation-rules", "create", "--body-file", "@body"}, `{"transformationRule":{"id":"x1"}}`, `{"id":"x1"}`},
		{"findings transformation-rules update", []string{"findings", "transformation-rules", "update", "x1", "--body-file", "@body"}, `{"transformationRule":{"id":"x1","displayName":"n"}}`, `{"id":"x1","displayName":"n"}`},
		{"findings transformation-rules delete", []string{"findings", "transformation-rules", "delete", "x1"}, `{}`, `{}`},
		{"role-mining trigger", []string{"role-mining", "trigger"}, `{"runId":"run1"}`, ""},
		{"role-mining suggestions state", []string{"role-mining", "suggestions", "state", "s1", "--body-file", "@body"}, `{"suggestion":{"id":"s1","suggestionState":"DISMISSED"}}`, `{"id":"s1","suggestionState":"DISMISSED"}`},
		{"role-mining config update", []string{"role-mining", "config", "update", "--body-file", "@body"}, `{"config":{"minCohortSize":10}}`, `{"minCohortSize":10}`},
		{"role-mining custom-analysis trigger", []string{"role-mining", "custom-analysis", "trigger", "--body-file", "@body"}, `{"id":"a1"}`, ""},
		{"role-mining custom-analysis evaluate", []string{"role-mining", "custom-analysis", "evaluate", "a1", "--body-file", "@body"}, `{"coverage":3}`, ""},
		{"role-mining access-profiles create", []string{"role-mining", "access-profiles", "create", "--body-file", "@body"}, `{"accessProfileId":"ap1","celExpression":"true"}`, ""},
		{"access-reviews create", []string{"access-reviews", "create", "--body-file", "@owners"}, `{"accessReview":{"accessReview":{"id":"ar1"},"policyPath":""},"expanded":[{"id":"p1"}]}`, `{"id":"ar1","expanded":[{"id":"p1"}],"policyPath":""}`},
		{"access-reviews update", []string{"access-reviews", "update", "ar1", "--body-file", "@body", "--update-mask", "description"}, `{"accessReview":{"accessReview":{"id":"ar1"},"policyPath":""}}`, `{"id":"ar1","policyPath":""}`},
		{"access-reviews reports generate", []string{"access-reviews", "reports", "generate", "ar1"}, `{}`, `{}`},
		{"policies create", []string{"policies", "create", "--display-name", "zz", "--policy-type", "grant", "--steps-file", "@steps"}, `{"policy":{"id":"pol1","displayName":"zz"}}`, `{"id":"pol1","displayName":"zz"}`},
		{"policies update", []string{"policies", "update", "pol1", "--display-name", "zz"}, `{"policy":{"id":"pol1","displayName":"zz"}}`, `{"id":"pol1","displayName":"zz"}`},
		{"policies delete", []string{"policies", "delete", "pol1"}, `{}`, `{}`},
	}
}

// govArgs swaps the "@..." placeholders for temp files.
func govArgs(t *testing.T, args []string) []string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"@body":   `{}`,
		"@owners": `{"displayName":"zz","ownerIds":["u1"]}`,
		"@steps":  `[{"approval":{"users":{"userIds":["u1"]}}},{"accept":{}}]`,
	}
	out := make([]string, len(args))
	for i, a := range args {
		if content, ok := files[a]; ok {
			p := filepath.Join(dir, strings.TrimPrefix(a, "@")+".json")
			if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			a = p
		}
		out[i] = a
	}
	return out
}

func indentedDoc(t *testing.T, compact string) string {
	t.Helper()
	var b bytes.Buffer
	if err := json.Indent(&b, []byte(compact), "", "  "); err != nil {
		t.Fatal(err)
	}
	return b.String() + "\n"
}

func TestGovMutationStdout(t *testing.T) {
	for _, tc := range govCases() {
		t.Run(tc.name, func(t *testing.T) {
			s := newMutServer(t, func(*http.Request, int) mutReply { return mutReply{body: tc.body} })
			t.Setenv("C1I_URL", mutTestURL)
			r := runMutationCLI(t, s.srv, 0, nil, govArgs(t, tc.args)...)
			if r.err != nil {
				t.Fatalf("exit %d: %v\n%s", r.exit, r.err, r.stderr)
			}
			want := tc.want
			if want == "" {
				want = tc.body
			}
			if got := indentedDoc(t, want); r.stdout != got {
				t.Errorf("stdout = %q, want %q", r.stdout, got)
			}
			if r.stderr != "" {
				t.Errorf("stderr = %q, want nothing", r.stderr)
			}
		})
	}
}

// create-task keeps its envelope: the ids are .taskId and .finding.id.
func TestFindingsCreateTaskKeepsIdentity(t *testing.T) {
	s := newMutServer(t, fixedReply(200, `{"finding":{"id":"f1"},"taskId":"t1"}`))
	t.Setenv("C1I_URL", mutTestURL)
	r := runMutationCLI(t, s.srv, 0, nil, "findings", "create-task", "f1")
	var doc struct {
		TaskID  string `json:"taskId"`
		Finding struct {
			ID string `json:"id"`
		} `json:"finding"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil || doc.TaskID != "t1" || doc.Finding.ID != "f1" {
		t.Errorf("stdout %q: want .taskId t1 and .finding.id f1 (err %v)", r.stdout, err)
	}
}

func TestGovMutationPreview(t *testing.T) {
	withDryRun(t)
	t.Setenv("C1I_URL", mutTestURL)
	r := runMutationCLI(t, nil, 0, nil, "findings", "routing-rules", "delete", "r/1")
	want := "{\n  \"dryRun\": true,\n  \"requests\": [\n    {\n      \"method\": \"DELETE\",\n      \"url\": \"" + mutTestURL + "/api/v1/findings/routing-rules/r%2F1\"\n    }\n  ]\n}\n"
	if r.err != nil || r.stdout != want {
		t.Errorf("exit %d stdout %q, want %q", r.exit, r.stdout, want)
	}
	if r.stderr != "dry run: nothing was sent\n" {
		t.Errorf("stderr = %q", r.stderr)
	}
}

func TestGovMutationRefusesFields(t *testing.T) {
	for _, tc := range govCases() {
		t.Run(tc.name, func(t *testing.T) {
			s := newMutServer(t, fixedReply(200, tc.body))
			t.Setenv("C1I_URL", mutTestURL)
			args := append(govArgs(t, tc.args), "--fields", "id")
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.exit != exitUsage || r.stdout != "" || len(s.requests()) != 0 {
				t.Errorf("exit %d stdout %q, %d requests; want exit 2 and nothing sent", r.exit, r.stdout, len(s.requests()))
			}
		})
	}
}

func TestGovMutationAppliedOn404(t *testing.T) {
	for _, tc := range govCases() {
		t.Run(tc.name, func(t *testing.T) {
			s := newMutServer(t, fixedReply(404, `{"code":5}`))
			t.Setenv("C1I_URL", mutTestURL)
			args := append(govArgs(t, tc.args), "--error-format", "json")
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.exit != exitNotFound || r.stdout != "" {
				t.Fatalf("exit %d stdout %q, want exit 4 and nothing on stdout", r.exit, r.stdout)
			}
			if got := r.errorObject(t)["applied"]; got != false {
				t.Errorf("applied = %v, want false", got)
			}
		})
	}
}

// A policies update whose patch needs the policy's type looks it up inside
// the helper; the lookup failing sends nothing and reports applied:false.
func TestPoliciesUpdateTypeLookup(t *testing.T) {
	stepsFile := func(t *testing.T) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "steps.json")
		if err := os.WriteFile(p, []byte(`[{"approval":{"users":{"userIds":["u1"]}}},{"accept":{}}]`), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
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
	t.Run("dry run cannot look up", func(t *testing.T) {
		withDryRun(t)
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, nil, 0, nil, "policies", "update", "pol1", "--steps-file", stepsFile(t))
		if r.exit != exitUsage || r.stdout != "" {
			t.Errorf("exit %d stdout %q, want exit 2 and nothing", r.exit, r.stdout)
		}
	})
}
