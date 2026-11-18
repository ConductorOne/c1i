package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestSettingsTestSourceIPExitsZeroForEitherAnswer(t *testing.T) {
	for _, tc := range []struct {
		name, reply string
		args        []string
		wantBody    string
	}{
		{"allowed", `{"allowed":true,"details":null,"checkedIp":"192.0.2.10"}`, []string{"--ip", "192.0.2.10", "--allow-cidr", "192.0.2.0/24", "--allow-cidr", "198.51.100.0/24"}, `{"allowCidr":["192.0.2.0/24","198.51.100.0/24"],"sourceIp":"192.0.2.10"}`},
		{"refused", `{"allowed":false,"details":{"code":7,"message":"unauthorized source ip","details":[]},"checkedIp":"198.51.100.1"}`, []string{"--ip", "198.51.100.1", "--allow-cidr", "192.0.2.0/24"}, `{"allowCidr":["192.0.2.0/24"],"sourceIp":"198.51.100.1"}`},
		{"no cidr", `{"allowed":false,"details":null,"checkedIp":"192.0.2.10"}`, []string{"--ip", "192.0.2.10"}, `{"sourceIp":"192.0.2.10"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, fixedReply(200, tc.reply))
			r := runMutationCLI(t, s.srv, 0, nil, append([]string{"settings", "session", "test-source-ip"}, tc.args...)...)
			if r.exit != 0 {
				t.Fatalf("exit %d (%v), want 0", r.exit, r.err)
			}
			reqs := s.requests()
			if len(reqs) != 1 || reqs[0].method != http.MethodPost || reqs[0].rawPath != "/api/v1/settings/session/test-source-ip" {
				t.Fatalf("requests = %+v, want one POST to test-source-ip", reqs)
			}
			if got := compactRaw(t, reqs[0].body); got != compactRaw(t, tc.wantBody) {
				t.Errorf("body = %s, want %s", got, tc.wantBody)
			}
			if compactRaw(t, r.stdout) != compactRaw(t, tc.reply) {
				t.Errorf("stdout = %s, want the response as returned", r.stdout)
			}
		})
	}
}

func TestSettingsTestSourceIPRefusesBlankCIDR(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, fixedReply(200, `{}`))
	r := runMutationCLI(t, s.srv, 0, nil, "settings", "session", "test-source-ip", "--ip", "192.0.2.1", "--allow-cidr", "")
	if r.exit != exitUsage || len(s.requests()) != 0 {
		t.Errorf("exit %d (%v), %d request(s); want exit 2, none", r.exit, r.err, len(s.requests()))
	}
}

// The AI governance history route takes no paging parameters: one GET, every
// record printed raw, and a cursor reported rather than silently dropped.
func TestSettingsAIGovernanceHistoryIsOneRead(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	reply := `{"list":[{"metadata":{"id":"h2","changeKind":"CHANGE_KIND_PUT"},"snapshot":{"auditVerbosity":"AUDIT_VERBOSITY_FULL"}},{"metadata":{"id":"h1"},"snapshot":{}}],"nextPageToken":"more"}`
	s := newMutServer(t, fixedReply(200, reply))
	r := runMutationCLI(t, s.srv, 0, nil, "settings", "ai-governance", "history")
	if r.exit != 0 {
		t.Fatalf("exit %d (%v)", r.exit, r.err)
	}
	reqs := s.requests()
	if len(reqs) != 1 || reqs[0].rawPath != "/api/v1/settings/ai-governance/history" {
		t.Fatalf("requests = %+v, want one GET of the history", reqs)
	}
	lines := strings.Split(strings.TrimSpace(r.stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("stdout = %q, want 2 records", r.stdout)
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil || first["snapshot"] == nil || first["metadata"] == nil {
		t.Errorf("first record = %s, want metadata and snapshot kept", lines[0])
	}
	if !strings.Contains(r.stderr, "partial result") {
		t.Errorf("stderr = %q, want a partial-result notice for the unexpected cursor", r.stderr)
	}
}

func TestSettingsHistoryFollowsCursor(t *testing.T) {
	for _, area := range []string{"sso", "cross-app-access"} {
		t.Run(area, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, func(r *http.Request, n int) mutReply {
				if r.URL.Query().Get("page_token") == "" {
					return mutReply{body: `{"list":[{"metadata":{"id":"h2"},"snapshot":{}}],"nextPageToken":"t2"}`}
				}
				return mutReply{body: `{"list":[{"metadata":{"id":"h1"},"snapshot":{}}],"nextPageToken":""}`}
			})
			r := runListCLI(t, s.srv, "settings", area, "history")
			if r.exit != 0 {
				t.Fatalf("exit %d (%v)", r.exit, r.err)
			}
			if n := len(strings.Split(strings.TrimSpace(r.stdout), "\n")); n != 2 || len(s.requests()) != 2 {
				t.Errorf("%d records from %d requests, want 2 from 2: %q", n, len(s.requests()), r.stdout)
			}
			for _, q := range s.requests() {
				if !strings.HasPrefix(q.rawPath, "/api/v1/settings/"+area+"/history") {
					t.Errorf("request path %q", q.rawPath)
				}
			}
		})
	}
}

func TestSettingsDomainsListRows(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, fixedReply(200, `{"list":[{"id":"d1","domain":"zz.example.com","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z","deletedAt":null}],"nextPageToken":""}`))
	r := runListCLI(t, s.srv, "settings", "domains", "list")
	if r.exit != 0 {
		t.Fatalf("exit %d (%v)", r.exit, r.err)
	}
	want := `{"created_at":"2026-01-01T00:00:00Z","deleted_at":null,"domain":"zz.example.com","id":"d1","updated_at":"2026-01-01T00:00:00Z"}`
	if got := compactRaw(t, r.stdout); got != want {
		t.Errorf("row = %s, want %s", got, want)
	}
}

func TestSettingsDomainsSetInputs(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		exit     int
		wantBody string
	}{
		{"domains", []string{"--domain", "a.example.com", "--domain", "b.example.com"}, 0, `{"newDomains":["a.example.com","b.example.com"]}`},
		{"empty set via body", []string{"--body-file", "@" + `{"newDomains":[]}`}, 0, `{"newDomains":[]}`},
		{"neither", nil, exitUsage, ""},
		{"both", []string{"--domain", "a.example.com", "--body-file", "@{}"}, exitUsage, ""},
		{"blank domain", []string{"--domain", "a.example.com", "--domain", " "}, exitUsage, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, fixedReply(200, `{"list":[]}`))
			args := []string{"settings", "domains", "set"}
			for _, a := range tc.args {
				if strings.HasPrefix(a, "@") {
					a = writeTempJSON(t, "body.json", a[1:])
				}
				args = append(args, a)
			}
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.exit != tc.exit {
				t.Fatalf("exit %d (%v), want %d", r.exit, r.err, tc.exit)
			}
			reqs := s.requests()
			if tc.wantBody == "" {
				if len(reqs) != 0 {
					t.Errorf("%d request(s) sent, want none", len(reqs))
				}
				return
			}
			if len(reqs) != 1 || reqs[0].method != http.MethodPut || compactRaw(t, reqs[0].body) != compactRaw(t, tc.wantBody) {
				t.Errorf("requests = %+v, want one PUT with %s", reqs, tc.wantBody)
			}
		})
	}
}

// The mask-less body updates refuse a body that names nothing, real run and
// --dry-run alike: with no mask its effect on the server is unmeasured.
func TestSettingsMasklessBodiesRefuseEmpty(t *testing.T) {
	type tc struct {
		cmd  []string
		body string
		ok   bool
	}
	notif := func(scope string) []string { return []string{"settings", "notifications", scope, "update"} }
	edge := []string{"findings", "settings", "edge", "update"}
	domains := []string{"settings", "domains", "set"}
	cases := []tc{
		{edge, `{}`, false},
		{edge, `{"settings":[]}`, false},
		{edge, `{"settings":null,"subcategorySettings":[]}`, false},
		{edge, `{"subcategorySettings":[{"subcategory":"S","enabled":false}]}`, true},
		{domains, `{}`, false},
		{domains, `{"newDomains":null}`, false},
		{domains, `{"newDomains":{}}`, false},
		{domains, `{"newDomains":[]}`, true},
	}
	for _, scope := range []string{"org", "user"} {
		cases = append(cases,
			tc{notif(scope), `{}`, false},
			tc{notif(scope), `{"channelSettings":{}}`, false},
			tc{notif(scope), `{"channelSettings":null}`, false},
			tc{notif(scope), `{"channelSettings":[]}`, false},
			tc{notif(scope), `{"channelSettings":{"email":{"enabled":true}}}`, true},
		)
	}
	for _, c := range cases {
		for _, dry := range []bool{false, true} {
			name := strings.Join(c.cmd, " ") + " " + c.body
			if dry {
				name += " --dry-run"
			}
			t.Run(name, func(t *testing.T) {
				t.Setenv("C1I_URL", mutTestURL)
				clearViperOverrides(t, "dry_run")
				s := newMutServer(t, fixedReply(200, `{}`))
				args := append(append([]string{}, c.cmd...), "--body-file", writeTempJSON(t, "body.json", c.body))
				if dry {
					args = append(args, "--dry-run")
				}
				r := runMutationCLI(t, s.srv, 0, nil, args...)
				if c.ok {
					if r.exit != 0 {
						t.Errorf("exit %d (%v), want 0", r.exit, r.err)
					}
					return
				}
				if r.exit != exitUsage || len(s.requests()) != 0 || r.stdout != "" {
					t.Errorf("exit %d (%v), %d request(s), stdout %q; want exit 2, nothing sent or previewed", r.exit, r.err, len(s.requests()), r.stdout)
				}
			})
		}
	}
}
