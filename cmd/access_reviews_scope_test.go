package cmd

import (
	"net/http"
	"strings"
	"testing"
)

// scopeReplyRoute answers GETs with an entitlement and writes with {} unless
// the GET path is in missing, which gets a 404.
func scopeReplyRoute(missing ...string) func(*http.Request, int) mutReply {
	return func(r *http.Request, _ int) mutReply {
		if r.Method == http.MethodGet {
			for _, m := range missing {
				if r.URL.EscapedPath() == m {
					return mutReply{status: http.StatusNotFound, body: `{"code":5,"message":"not found"}`}
				}
			}
			return mutReply{body: `{"appEntitlementView":{"appEntitlement":{"id":"x"}}}`}
		}
		return mutReply{body: `{}`}
	}
}

// The flag commands send --app-id with each --entitlement-id under the key
// the operation names, keyed appEntitlementId, after one lookup per distinct
// entitlement.
func TestAccessReviewSetupRefCommandsWire(t *testing.T) {
	cases := []struct {
		cmd, path, key string
	}{
		{"entitlements add", "/api/v1/access_review/ar%2F1/entitlements/add", "entitlements"},
		{"entitlements remove", "/api/v1/access_review/ar%2F1/entitlements/remove", "entitlements"},
		{"excluded-entitlements add", "/api/v1/access_review/ar%2F1/excluded_entitlements/add_delete", "toAdd"},
		{"excluded-entitlements remove", "/api/v1/access_review/ar%2F1/excluded_entitlements/add_delete", "toDelete"},
	}
	for _, tc := range cases {
		t.Run(tc.cmd, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, scopeReplyRoute())
			args := append([]string{"access-reviews"}, strings.Fields(tc.cmd)...)
			args = append(args, "ar/1", "--app-id", "app 1", "--entitlement-id", "ent-1", "--entitlement-id", "ent/2", "--entitlement-id", "ent-1")
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.err != nil {
				t.Fatalf("exit %d: %v\n%s", r.exit, r.err, r.stderr)
			}
			sent := s.requests()
			want := []entOwnerRequest{
				{http.MethodGet, "/api/v1/apps/app%201/entitlements/ent-1", ""},
				{http.MethodGet, "/api/v1/apps/app%201/entitlements/ent%2F2", ""},
				{http.MethodPost, tc.path, `{"` + tc.key + `":[{"appId":"app 1","appEntitlementId":"ent-1"},{"appId":"app 1","appEntitlementId":"ent/2"},{"appId":"app 1","appEntitlementId":"ent-1"}]}`},
			}
			if len(sent) != len(want) {
				t.Fatalf("requests = %+v, want %+v", sent, want)
			}
			for i := range want {
				if sent[i] != want[i] {
					t.Errorf("request %d = %+v, want %+v", i, sent[i], want[i])
				}
			}
		})
	}
}

// An unknown referenced entitlement or resource type exits 4 before the
// write, with --dry-run too: the server answers 400, 500 or a silent skip.
func TestAccessReviewSetupUnknownRefExitsNotFound(t *testing.T) {
	const ent = "/api/v1/apps/app-1/entitlements/ent-9"
	const rt = "/api/v1/apps/app-1/resource_types/rt-9"
	cases := []struct {
		name    string
		args    []string
		missing string
		inErr   string
	}{
		{"entitlements add", []string{"entitlements", "add", "ar1", "--app-id", "app-1", "--entitlement-id", "ent-1", "--entitlement-id", "ent-9"}, ent, `--entitlement-id "ent-9" under --app-id "app-1": no such entitlement`},
		{"entitlements remove", []string{"entitlements", "remove", "ar1", "--app-id", "app-1", "--entitlement-id", "ent-9"}, ent, "no such entitlement"},
		{"excluded add", []string{"excluded-entitlements", "add", "ar1", "--app-id", "app-1", "--entitlement-id", "ent-9"}, ent, "no such entitlement"},
		{"excluded remove", []string{"excluded-entitlements", "remove", "ar1", "--app-id", "app-1", "--entitlement-id", "ent-9"}, ent, "no such entitlement"},
		{"scope set", []string{"scope", "set", "ar1", "--body-file", `@{"scopeV2":{},"entitlements":[{"appId":"app-1","appEntitlementId":"ent-1"},{"appId":"app-1","appEntitlementId":"ent-9"}]}`}, ent, `--body-file entitlements[1] (appId "app-1", appEntitlementId "ent-9"): no such entitlement`},
		{"scope set-excluded", []string{"scope", "set-excluded", "ar1", "--body-file", `@{"scopeV2":{},"excludedEntitlements":[{"appId":"app-1","appEntitlementId":"ent-9"}]}`}, ent, "no such entitlement"},
		{"scope set-by-resource-type", []string{"scope", "set-by-resource-type", "ar1", "--body-file", `@{"scopeV2":{},"resourceTypeSelections":[{"appId":"app-1","resourceTypeId":"rt-9"}]}`}, rt, "no such resource type"},
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
				s := newMutServer(t, scopeReplyRoute(tc.missing))
				row := mutationRow{cmd: "access-reviews", args: tc.args}
				r := runMutationCLI(t, s.srv, 0, nil, row.argv(t)...)
				if r.exit != exitNotFound || !strings.Contains(r.stderr, tc.inErr) {
					t.Errorf("exit %d, stderr %q; want exit %d naming %q", r.exit, r.stderr, exitNotFound, tc.inErr)
				}
				if r.stdout != "" {
					t.Errorf("stdout = %q, want nothing", r.stdout)
				}
				for _, q := range s.requests() {
					if q.method != http.MethodGet {
						t.Errorf("sent %s %s after an unknown reference", q.method, q.rawPath)
					}
				}
			})
		}
	}
}

// A body file's reference array must be an array of exact {appId, <id key>}
// objects; anything else is a usage error before any request.
func TestAccessReviewScopeBodyFileRefShape(t *testing.T) {
	cases := []struct{ cmd, body, inErr string }{
		{"set", `{"entitlements":{"appId":"a"}}`, "--body-file entitlements must be a JSON array, not an object"},
		{"set", `{"entitlements":["ent-1"]}`, "--body-file entitlements element 0 must be a JSON object, not a string"},
		{"set", `{"entitlements":[{"appId":"a","id":"e"}]}`, "--body-file entitlements[0] must be an AccessReviewSetupEntitlementInput with non-empty appId and appEntitlementId"},
		{"set", `{"entitlements":[{"appId":"a","appEntitlementId":"e","x":1}]}`, "exactly appId and appEntitlementId"},
		{"set-excluded", `{"excludedEntitlements":[{"appId":"","appEntitlementId":"e"}]}`, "AccessReviewExcludedEntitlementInput with non-empty appId"},
		{"set-by-resource-type", `{"resourceTypeSelections":[{"appId":"a"}]}`, "ResourceTypeIdRef with exactly appId and resourceTypeId"},
	}
	for _, tc := range cases {
		t.Run(tc.cmd+" "+tc.body, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			row := mutationRow{cmd: "access-reviews scope " + tc.cmd, args: []string{"ar1", "--body-file", "@" + tc.body}}
			r := runMutationCLI(t, nil, 0, nil, row.argv(t)...)
			if r.exit != exitUsage || !strings.Contains(r.stderr, tc.inErr) {
				t.Errorf("exit %d, stderr %q; want exit %d naming %q", r.exit, r.stderr, exitUsage, tc.inErr)
			}
		})
	}
}

// A lookup that fails for another reason keeps its own exit and stops the write.
func TestAccessReviewSetupRefLookupFailureStopsWrite(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, func(r *http.Request, _ int) mutReply {
		if r.Method == http.MethodGet {
			return mutReply{status: http.StatusForbidden, body: `{"code":7,"message":"denied"}`}
		}
		return mutReply{body: `{}`}
	})
	r := runMutationCLI(t, s.srv, 0, nil, "access-reviews", "entitlements", "add", "ar1", "--app-id", "app-1", "--entitlement-id", "ent-1")
	if r.exit != exitAuth {
		t.Errorf("exit %d (%v), want %d", r.exit, r.err, exitAuth)
	}
	if got := len(s.requests()); got != 1 {
		t.Errorf("sent %d requests, want only the lookup", got)
	}
}
