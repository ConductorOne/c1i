package cmd

import (
	"net/http"
	"strings"
	"testing"
)

// templateRoute answers the template, user and policy reads; a GET whose
// escaped path is in missing gets a 404. Writes echo a template.
func templateRoute(policyType string, missing ...string) func(*http.Request, int) mutReply {
	return func(r *http.Request, _ int) mutReply {
		if r.Method != http.MethodGet {
			return mutReply{body: `{"accessReviewTemplate":{"id":"t1","displayName":"zz"}}`}
		}
		for _, m := range missing {
			if r.URL.EscapedPath() == m {
				return mutReply{status: http.StatusNotFound, body: `{"code":5,"message":"not found"}`}
			}
		}
		p := r.URL.Path
		switch {
		case strings.HasPrefix(p, "/api/v1/policies/"):
			return mutReply{body: `{"policy":{"id":"pol1","policyType":"` + policyType + `"}}`}
		case strings.HasPrefix(p, "/api/v1/users/"):
			return mutReply{body: `{"user":{"id":"u1"}}`}
		case strings.HasPrefix(p, "/api/v1/access_review_template/"):
			return mutReply{body: `{"accessReviewTemplate":{"id":"t1","displayName":"cur","policyId":"pol0"}}`}
		}
		return mutReply{body: `{}`}
	}
}

func writesOf(s *mutServer) []entOwnerRequest {
	var out []entOwnerRequest
	for _, q := range s.requests() {
		if q.method != http.MethodGet {
			out = append(out, q)
		}
	}
	return out
}

func TestTemplateCreateFlagsWire(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, templateRoute("POLICY_TYPE_CERTIFY"))
	r := runMutationCLI(t, s.srv, 0, nil, "access-reviews", "templates", "create", "--display-name", "zz", "--policy-id", "pol1",
		"--owner-id", "u1", "--owner-id", "u/2", "--review-instructions", "look", "--scope-type", "by-users")
	if r.err != nil {
		t.Fatalf("exit %d: %v\n%s", r.exit, r.err, r.stderr)
	}
	want := []entOwnerRequest{
		{http.MethodGet, "/api/v1/policies/pol1", ""},
		{http.MethodGet, "/api/v1/users/u1", ""},
		{http.MethodGet, "/api/v1/users/u%2F2", ""},
		{http.MethodPost, "/api/v1/access_review_template", `{"displayName":"zz","ownerIds":["u1","u/2"],"policyId":"pol1","reviewInstructions":"look","scopeType":"ACCESS_REVIEW_SCOPE_TYPE_BY_USERS"}`},
	}
	sent := s.requests()
	if len(sent) != len(want) {
		t.Fatalf("requests = %+v, want %+v", sent, want)
	}
	for i := range want {
		if sent[i] != want[i] {
			t.Errorf("request %d = %+v, want %+v", i, sent[i], want[i])
		}
	}
}

// Each create mode requires its own inputs and refuses mixing them, before
// any request.
func TestTemplateCreateUsage(t *testing.T) {
	cases := []struct {
		args  []string
		inErr string
	}{
		{[]string{"--policy-id", "p", "--owner-id", "u"}, "--display-name is required (or pass --body-file)"},
		{[]string{"--display-name", "n", "--owner-id", "u"}, "--policy-id is required (or pass --body-file)"},
		{[]string{"--display-name", "n", "--policy-id", "p"}, "--owner-id is required (or pass --body-file)"},
		{[]string{"--body-file", `@{"ownerIds":["u"]}`, "--display-name", "n"}, "--body-file is mutually exclusive with --display-name"},
		{[]string{"--body-file", `@{"policyId":"p"}`}, "ownerIds must contain at least one user ID"},
		{[]string{"--body-file", `@{"ownerIds":["u"],"policyId":" "}`}, "--body-file policyId must be a non-blank policy id string"},
		{[]string{"--display-name", "n", "--policy-id", "p", "--owner-id", "u", "--scope-type", "sideways"}, "--scope-type"},
	}
	for _, tc := range cases {
		t.Run(tc.inErr, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, templateRoute("POLICY_TYPE_CERTIFY"))
			row := mutationRow{cmd: "access-reviews templates create", args: tc.args}
			r := runMutationCLI(t, s.srv, 0, nil, row.argv(t)...)
			if r.exit != exitUsage || !strings.Contains(r.stderr, tc.inErr) {
				t.Errorf("exit %d, stderr %q; want exit %d naming %q", r.exit, r.stderr, exitUsage, tc.inErr)
			}
			if n := len(s.requests()); n != 0 {
				t.Errorf("sent %d requests", n)
			}
		})
	}
}

// The server stores an unknown owner and a policy of any type, so both are
// looked up on every channel; nothing is written, --dry-run included.
func TestTemplateRefChecks(t *testing.T) {
	flags := []string{"create", "--display-name", "zz", "--policy-id", "pol1", "--owner-id", "u1"}
	body := []string{"create", "--body-file", `@{"displayName":"zz","policyId":"pol1","ownerIds":["u1"]}`}
	cases := []struct {
		name       string
		args       []string
		policyType string
		missing    string
		exit       int
		inErr      string
	}{
		{"flag owner unknown", flags, "POLICY_TYPE_CERTIFY", "/api/v1/users/u1", exitNotFound, `--owner-id "u1": no such user`},
		{"body owner unknown", body, "POLICY_TYPE_CERTIFY", "/api/v1/users/u1", exitNotFound, `--body-file ownerIds "u1": no such user`},
		{"flag policy unknown", flags, "POLICY_TYPE_CERTIFY", "/api/v1/policies/pol1", exitNotFound, `--policy-id "pol1": no such policy`},
		{"body policy unknown", body, "POLICY_TYPE_CERTIFY", "/api/v1/policies/pol1", exitNotFound, `--body-file policyId "pol1": no such policy`},
		{"flag policy grant", flags, "POLICY_TYPE_GRANT", "", exitUsage, "is a POLICY_TYPE_GRANT policy; it must be POLICY_TYPE_CERTIFY"},
		{"body policy grant", body, "POLICY_TYPE_GRANT", "", exitUsage, "is a POLICY_TYPE_GRANT policy"},
		{"update flag policy grant", []string{"update", "t1", "--policy-id", "pol1"}, "POLICY_TYPE_GRANT", "", exitUsage, "is a POLICY_TYPE_GRANT policy"},
		{"update body policy unknown", []string{"update", "t1", "--body-file", `@{"policyId":"pol1"}`, "--update-mask", "policyId"}, "POLICY_TYPE_CERTIFY", "/api/v1/policies/pol1", exitNotFound, `--body-file policyId "pol1": no such policy`},
		{"update template unknown", []string{"update", "t9", "--description", "d"}, "POLICY_TYPE_CERTIFY", "/api/v1/access_review_template/t9", exitNotFound, "not found"},
	}
	for _, tc := range cases {
		for _, dry := range []bool{false, true} {
			name := tc.name
			if dry {
				name += " dry-run"
			}
			t.Run(name, func(t *testing.T) {
				t.Setenv("C1I_URL", mutTestURL)
				if dry {
					withDryRun(t)
				}
				s := newMutServer(t, templateRoute(tc.policyType, tc.missing))
				row := mutationRow{cmd: "access-reviews templates", args: tc.args}
				r := runMutationCLI(t, s.srv, 0, nil, row.argv(t)...)
				if r.exit != tc.exit || !strings.Contains(r.stderr, tc.inErr) {
					t.Errorf("exit %d, stderr %q; want exit %d naming %q", r.exit, r.stderr, tc.exit, tc.inErr)
				}
				if r.stdout != "" {
					t.Errorf("stdout = %q, want nothing", r.stdout)
				}
				if w := writesOf(s); len(w) != 0 {
					t.Errorf("wrote %+v", w)
				}
			})
		}
	}
}

// update fills displayName and policyId from the current template only where
// the request leaves them unset; a body file's own values are kept.
func TestTemplateUpdateFillsRequiredFields(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"flags", []string{"t1", "--description", "d"},
			`{"accessReviewTemplate":{"description":"d","displayName":"cur","id":"t1","policyId":"pol0"},"updateMask":"description"}`},
		{"body keeps its own", []string{"t1", "--body-file", `@{"displayName":"mine","policyId":"pol1","description":"d"}`, "--update-mask", "description"},
			`{"accessReviewTemplate":{"description":"d","displayName":"mine","id":"t1","policyId":"pol1"},"updateMask":"description"}`},
		{"body fills the missing one", []string{"t1", "--body-file", `@{"displayName":"mine"}`, "--update-mask", "displayName"},
			`{"accessReviewTemplate":{"displayName":"mine","id":"t1","policyId":"pol0"},"updateMask":"displayName"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, templateRoute("POLICY_TYPE_CERTIFY"))
			row := mutationRow{cmd: "access-reviews templates update", args: tc.args}
			r := runMutationCLI(t, s.srv, 0, nil, row.argv(t)...)
			if r.err != nil {
				t.Fatalf("exit %d: %v\n%s", r.exit, r.err, r.stderr)
			}
			w := writesOf(s)
			if len(w) != 1 || w[0].body != tc.want {
				t.Errorf("writes = %+v, want one with body %s", w, tc.want)
			}
		})
	}
}

// A template read that holds no template, or no displayName, exits 6 unsent.
func TestTemplateUpdateUnrecognizedReadExits6(t *testing.T) {
	for _, read := range []string{`{}`, `{"accessReviewTemplate":{"id":"t1","policyId":"p"}}`} {
		t.Run(read, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, func(r *http.Request, _ int) mutReply {
				if r.Method == http.MethodGet {
					return mutReply{body: read}
				}
				return mutReply{body: `{}`}
			})
			r := runMutationCLI(t, s.srv, 0, nil, "access-reviews", "templates", "update", "t1", "--description", "d")
			if r.exit != exitServer {
				t.Errorf("exit %d (%v), want %d", r.exit, r.err, exitServer)
			}
			if w := writesOf(s); len(w) != 0 {
				t.Errorf("wrote %+v", w)
			}
		})
	}
}

// The template scope commands reuse the campaign builders on the template
// path, keyed by scope, with the same reference lookups.
func TestTemplateSetupCommandsWire(t *testing.T) {
	cases := []struct {
		args       []string
		path, body string
	}{
		{[]string{"entitlements", "add", "t/1", "--app-id", "a1", "--entitlement-id", "e1"}, "/api/v1/access_review_template/t%2F1/entitlements/add", `{"entitlements":[{"appId":"a1","appEntitlementId":"e1"}]}`},
		{[]string{"entitlements", "remove", "t/1", "--app-id", "a1", "--entitlement-id", "e1"}, "/api/v1/access_review_template/t%2F1/entitlements/remove", `{"entitlements":[{"appId":"a1","appEntitlementId":"e1"}]}`},
		{[]string{"excluded-entitlements", "add", "t/1", "--app-id", "a1", "--entitlement-id", "e1"}, "/api/v1/access_review_template/t%2F1/excluded_entitlements/add_delete", `{"toAdd":[{"appId":"a1","appEntitlementId":"e1"}]}`},
		{[]string{"excluded-entitlements", "remove", "t/1", "--app-id", "a1", "--entitlement-id", "e1"}, "/api/v1/access_review_template/t%2F1/excluded_entitlements/add_delete", `{"toDelete":[{"appId":"a1","appEntitlementId":"e1"}]}`},
		{[]string{"scope", "set", "t/1", "--body-file", `@{"scope":{},"entitlements":[{"appId":"a1","appEntitlementId":"e1"}]}`}, "/api/v1/access_review_template/t%2F1/scope_and_entitlements", `{"entitlements":[{"appEntitlementId":"e1","appId":"a1"}],"scope":{}}`},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args[:2], " "), func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, scopeReplyRoute())
			row := mutationRow{cmd: "access-reviews templates", args: tc.args}
			r := runMutationCLI(t, s.srv, 0, nil, row.argv(t)...)
			if r.err != nil {
				t.Fatalf("exit %d: %v\n%s", r.exit, r.err, r.stderr)
			}
			sent := s.requests()
			want := []entOwnerRequest{
				{http.MethodGet, "/api/v1/apps/a1/entitlements/e1", ""},
				{http.MethodPost, tc.path, tc.body},
			}
			if len(sent) != 2 || sent[0] != want[0] || sent[1] != want[1] {
				t.Errorf("requests = %+v, want %+v", sent, want)
			}
		})
	}
}

// withTemplateReads answers the template read "templates update" makes before
// its write, so a tree test's fixed reply still reaches the write.
func withTemplateReads(route func(*http.Request, int) mutReply) func(*http.Request, int) mutReply {
	return func(r *http.Request, n int) mutReply {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/access_review_template/") {
			return mutReply{body: `{"accessReviewTemplate":{"id":"t1","displayName":"cur","policyId":"pol0"}}`}
		}
		return route(r, n)
	}
}

// A body that sets both fields the server validates needs no read, and a
// policyId the mask does not name is not stored, so it is not looked up.
func TestTemplateUpdateReadsOnlyWhatItNeeds(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, templateRoute("POLICY_TYPE_GRANT"))
	row := mutationRow{cmd: "access-reviews templates update", args: []string{"t1", "--body-file", `@{"displayName":"n","policyId":"pol1","description":"d"}`, "--update-mask", "description"}}
	r := runMutationCLI(t, s.srv, 0, nil, row.argv(t)...)
	if r.err != nil {
		t.Fatalf("exit %d: %v\n%s", r.exit, r.err, r.stderr)
	}
	if sent := s.requests(); len(sent) != 1 || sent[0].method != http.MethodPost {
		t.Errorf("requests = %+v, want the update alone", sent)
	}
}

// A mask naming displayName or policyId with no value for it in the body file
// would have the fill send the current value back: exit 2, nothing sent,
// --dry-run included. A named policyId in the body is still looked up.
func TestTemplateUpdateMaskedRequiredFieldNeedsBodyValue(t *testing.T) {
	cases := []struct {
		name, body, mask, policyType string
		exit                         int
		inErr                        string
	}{
		{"displayName missing", `{"description":"q"}`, "displayName", "POLICY_TYPE_CERTIFY", exitUsage, "--update-mask names displayName but --body-file sets no displayName"},
		{"policyId missing", `{"description":"q"}`, "description,policyId", "POLICY_TYPE_CERTIFY", exitUsage, "--update-mask names policyId but --body-file sets no policyId"},
		{"policyId grant", `{"policyId":"pol1"}`, "policyId", "POLICY_TYPE_GRANT", exitUsage, "is a POLICY_TYPE_GRANT policy"},
	}
	for _, tc := range cases {
		for _, dry := range []bool{false, true} {
			name := tc.name
			if dry {
				name += " dry-run"
			}
			t.Run(name, func(t *testing.T) {
				t.Setenv("C1I_URL", mutTestURL)
				if dry {
					withDryRun(t)
				}
				s := newMutServer(t, templateRoute(tc.policyType))
				row := mutationRow{cmd: "access-reviews templates update", args: []string{"t1", "--body-file", "@" + tc.body, "--update-mask", tc.mask}}
				r := runMutationCLI(t, s.srv, 0, nil, row.argv(t)...)
				if r.exit != tc.exit || !strings.Contains(r.stderr, tc.inErr) {
					t.Errorf("exit %d, stderr %q; want exit %d naming %q", r.exit, r.stderr, tc.exit, tc.inErr)
				}
				if r.stdout != "" {
					t.Errorf("stdout = %q, want nothing", r.stdout)
				}
				if w := writesOf(s); len(w) != 0 {
					t.Errorf("wrote %+v", w)
				}
			})
		}
	}
}
