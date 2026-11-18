package cmd

import (
	"net/http"
	"strings"
	"sync"
	"testing"
)

// entOwnerFamilies are the owned objects whose owner verbs take
// --owner-entitlement-id, with each one's v1 args and v2 owner base path.
var entOwnerFamilies = []struct {
	name   string
	prefix []string // command path
	args   []string // the positional and scope flags
	v2     string
}{
	{"apps", []string{"apps"}, []string{"app-1"}, "/api/v2/apps/app-1"},
	{"entitlements", []string{"entitlements"}, []string{"e-1", "--app-id", "app-1"}, "/api/v2/apps/app-1/entitlements/e-1"},
	{"resources", []string{"apps", "resources"}, []string{"r-1", "--app-id", "app-1", "--resource-type-id", "rt-1"}, "/api/v2/apps/app-1/resource_types/rt-1/resources/r-1"},
}

func entOwnerArgs(prefix []string, verb string, rest ...[]string) []string {
	out := append(append([]string{}, prefix...), verb)
	for _, r := range rest {
		out = append(out, r...)
	}
	return out
}

// TestEntitlementOwnerWire pins each family's entitlement-owner requests:
// the v2 route under role "primary", the ref in path and body, no user
// lookup, and --owner-entitlement-app-id overriding the owned object's app.
func TestEntitlementOwnerWire(t *testing.T) {
	for _, f := range entOwnerFamilies {
		cases := []struct {
			verb   string
			extra  []string
			method string
			path   string
			body   string
		}{
			{"add-owner", []string{"--owner-entitlement-id", "e-9"}, "POST", f.v2 + "/owners/entitlements/primary/app-1/e-9", `{"appEntitlementRef":{"appId":"app-1","id":"e-9"}}`},
			{"add-owner", []string{"--owner-entitlement-id", "e-9", "--owner-entitlement-app-id", "app-2"}, "POST", f.v2 + "/owners/entitlements/primary/app-2/e-9", `{"appEntitlementRef":{"appId":"app-2","id":"e-9"}}`},
			{"remove-owner", []string{"--owner-entitlement-id", "e/9"}, "DELETE", f.v2 + "/owners/entitlements/primary/app-1/e%2F9", ``},
			{"set-owners", []string{"--owner-entitlement-id", "e-9", "--owner-entitlement-id", "e-8", "--owner-entitlement-id", "e-9", "--no-user-owners"}, "PUT", f.v2 + "/owners", `{"appEntitlementRefs":[{"appId":"app-1","id":"e-9"},{"appId":"app-1","id":"e-8"}],"roleSlug":"primary","userRefs":[]}`},
		}
		for _, tc := range cases {
			t.Run(f.name+" "+tc.verb+" "+strings.Join(tc.extra, " "), func(t *testing.T) {
				t.Setenv("C1I_URL", mutTestURL)
				s := newMutServer(t, withLiveRefs(func(*http.Request, int) mutReply { return mutReply{body: `{}`} }))
				r := runMutationCLI(t, s.srv, 0, nil, entOwnerArgs(f.prefix, tc.verb, f.args, tc.extra)...)
				if r.exit != 0 {
					t.Fatalf("exit = %d (%v)", r.exit, r.err)
				}
				// apps and resources pre-read the app; resources set-owners
				// also reads its resource.
				got, reads := s.requests(), 0
				if f.name == "apps" || f.name == "resources" {
					reads++
				}
				if f.name == "resources" && tc.verb == "set-owners" {
					reads++
				}
				if len(got) != reads+1 {
					t.Fatalf("sent %d requests, want %d: %+v", len(got), reads+1, got)
				}
				w := got[reads]
				if w.method != tc.method || w.rawPath != tc.path || w.body != tc.body {
					t.Errorf("sent %s %s %s\nwant %s %s %s", w.method, w.rawPath, w.body, tc.method, tc.path, tc.body)
				}
			})
		}
	}
}

// TestSetOwnersEntitlementFormSendsUsers pins that the v2 set carries the
// --user-id list too, after the same user lookup as the v1 set.
func TestSetOwnersEntitlementFormSendsUsers(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, withLiveRefs(func(r *http.Request, _ int) mutReply {
		if r.Method == http.MethodGet {
			return mutReply{body: `{"user":{"id":"u1"}}`}
		}
		return mutReply{body: `{}`}
	}))
	r := runMutationCLI(t, s.srv, 0, nil, "apps", "set-owners", "app-1", "--user-id", "u1", "--user-id", "u1", "--owner-entitlement-id", "e-9")
	if r.exit != 0 {
		t.Fatalf("exit = %d (%v)", r.exit, r.err)
	}
	got := s.requests()
	if len(got) != 3 || got[0].method != "GET" || got[1].method != "GET" || got[2].method != "PUT" {
		t.Fatalf("requests = %+v, want app read, user lookup, PUT", got)
	}
	if want := `{"appEntitlementRefs":[{"appId":"app-1","id":"e-9"}],"roleSlug":"primary","userRefs":[{"id":"u1"}]}`; got[2].body != want || got[2].rawPath != "/api/v2/apps/app-1/owners" {
		t.Errorf("PUT %s %s, want /api/v2/apps/app-1/owners %s", got[2].rawPath, got[2].body, want)
	}
}

// TestEntitlementOwnerModeUsage pins the mode errors: each exits 2 and sends
// nothing.
func TestEntitlementOwnerModeUsage(t *testing.T) {
	for _, f := range entOwnerFamilies {
		cases := []struct {
			verb  string
			extra []string
			want  string
		}{
			{"add-owner", []string{"--user-id", "u1", "--owner-entitlement-id", "e-9"}, "mutually exclusive"},
			{"remove-owner", []string{"--user-id", "u1", "--owner-entitlement-id", "e-9"}, "mutually exclusive"},
			{"add-owner", nil, "one of --user-id or --owner-entitlement-id is required"},
			{"remove-owner", nil, "one of --user-id or --owner-entitlement-id is required"},
			{"set-owners", nil, "at least one --user-id or --owner-entitlement-id is required"},
			{"add-owner", []string{"--user-id", "u1", "--owner-entitlement-app-id", "app-2"}, "--owner-entitlement-app-id requires --owner-entitlement-id"},
			{"remove-owner", []string{"--user-id", "u1", "--owner-entitlement-app-id", "app-2"}, "--owner-entitlement-app-id requires --owner-entitlement-id"},
			{"set-owners", []string{"--user-id", "u1", "--owner-entitlement-app-id", "app-2"}, "--owner-entitlement-app-id requires --owner-entitlement-id"},
			{"add-owner", []string{"--owner-entitlement-id", "e-9", "--owner-entitlement-id", "e-8"}, "can be given once"},
			{"add-owner", []string{"--owner-entitlement-id", " "}, "--owner-entitlement-id"},
			{"set-owners", []string{"--owner-entitlement-id", ""}, "--owner-entitlement-id"},
		}
		for _, tc := range cases {
			t.Run(f.name+" "+tc.verb+" "+strings.Join(tc.extra, " "), func(t *testing.T) {
				t.Setenv("C1I_URL", mutTestURL)
				s := newMutServer(t, withLiveRefs(func(*http.Request, int) mutReply { return mutReply{body: `{}`} }))
				r := runMutationCLI(t, s.srv, 0, nil, entOwnerArgs(f.prefix, tc.verb, f.args, tc.extra)...)
				if r.exit != exitUsage {
					t.Errorf("exit = %d, want %d (%v)", r.exit, exitUsage, r.err)
				}
				if r.err == nil || !strings.Contains(r.err.Error(), tc.want) {
					t.Errorf("error %v, want it to contain %q", r.err, tc.want)
				}
				if n := len(s.requests()); n != 0 {
					t.Errorf("sent %d request(s); a usage error sends nothing", n)
				}
			})
		}
	}
}

// TestOwnersEntitlementsList pins "owners --entitlements": the v2 route with
// role_slug=primary, every page followed, and typed rows.
func TestOwnersEntitlementsList(t *testing.T) {
	for _, f := range entOwnerFamilies {
		t.Run(f.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			var mu sync.Mutex
			var queries []string
			s := newMutServer(t, func(r *http.Request, n int) mutReply {
				mu.Lock()
				defer mu.Unlock()
				queries = append(queries, r.URL.Query().Get("role_slug")+"|"+r.URL.Query().Get("page_token"))
				if n == 1 {
					return mutReply{body: `{"list":[{"appEntitlement":{"id":"e-9","appId":"app-1","displayName":"zz","deletedAt":null},"roleSlug":"primary","createdAt":"2026-01-01T00:00:00Z"}],"nextPageToken":"p2"}`}
				}
				return mutReply{body: `{"list":[{"appEntitlement":{"id":"e-8","appId":"app-2","slug":"member"},"roleSlug":"primary"}],"nextPageToken":""}`}
			})
			r := runListCLI(t, s.srv, entOwnerArgs(f.prefix, "owners", f.args, []string{"--entitlements"})...)
			if r.exit != 0 {
				t.Fatalf("exit = %d (%v)", r.exit, r.err)
			}
			got := s.requests()
			if len(got) != 2 || got[0].rawPath != f.v2+"/owners/entitlements" {
				t.Fatalf("requests = %+v, want two pages of %s/owners/entitlements", got, f.v2)
			}
			mu.Lock()
			defer mu.Unlock()
			if strings.Join(queries, ",") != "primary|,primary|p2" {
				t.Errorf("queries = %v", queries)
			}
			want := `{"added_at":"2026-01-01T00:00:00Z","alias":null,"app_id":"app-1","deleted_at":null,"display_name":"zz","id":"e-9","role_slug":"primary","slug":null}` + "\n" +
				`{"added_at":null,"alias":null,"app_id":"app-2","deleted_at":null,"display_name":null,"id":"e-8","role_slug":"primary","slug":"member"}` + "\n"
			if r.stdout != want {
				t.Errorf("stdout:\n%s\nwant:\n%s", r.stdout, want)
			}
		})
	}
}

// TestAddEntitlementOwnerOutputUnwrapped pins that the create response is
// printed without its family-specific envelope.
func TestAddEntitlementOwnerOutputUnwrapped(t *testing.T) {
	envelopes := map[string]string{"apps": "appOwnerEntitlement", "entitlements": "appEntitlementOwnerEntitlement", "resources": "appResourceOwnerEntitlement"}
	for _, f := range entOwnerFamilies {
		t.Run(f.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, withLiveRefs(func(*http.Request, int) mutReply {
				return mutReply{body: `{"` + envelopes[f.name] + `":{"appId":"app-1","appEntitlement":{"id":"e-9"},"roleSlug":"primary"}}`}
			}))
			r := runMutationCLI(t, s.srv, 0, nil, entOwnerArgs(f.prefix, "add-owner", f.args, []string{"--owner-entitlement-id", "e-9"})...)
			if r.exit != 0 {
				t.Fatalf("exit = %d (%v)", r.exit, r.err)
			}
			want := "{\n  \"appId\": \"app-1\",\n  \"appEntitlement\": {\n    \"id\": \"e-9\"\n  },\n  \"roleSlug\": \"primary\"\n}\n"
			if r.stdout != want {
				t.Errorf("stdout:\n%s\nwant:\n%s", r.stdout, want)
			}
		})
	}
}

// TestSetOwnersEntitlementFormWait pins that --wait on the v2 set polls the
// v1 ownerids for users and the v2 list for entitlements until both equal the
// request, and keeps polling while a replaced user lingers in ownerids.
func TestSetOwnersEntitlementFormWait(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	var mu sync.Mutex
	idPolls := 0
	s := newMutServer(t, func(r *http.Request, _ int) mutReply {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodPut:
			return mutReply{body: `{}`}
		case r.URL.Path == "/api/v1/apps/app-1/entitlements/e-1/ownerids":
			idPolls++
			if idPolls == 1 {
				return mutReply{body: `{"userIds":["u-old"]}`}
			}
			return mutReply{body: `{"userIds":[]}`}
		case r.URL.Path == "/api/v2/apps/app-1/entitlements/e-1/owners/entitlements":
			return mutReply{body: `{"list":[{"appEntitlement":{"id":"e-9","appId":"app-1"}}]}`}
		}
		return mutReply{status: http.StatusTeapot, body: `{}`}
	})
	r := runMutationCLI(t, s.srv, 0, nil, "entitlements", "set-owners", "e-1", "--app-id", "app-1", "--owner-entitlement-id", "e-9", "--no-user-owners", "--wait", "--wait-timeout", "5s")
	if r.exit != 0 {
		t.Fatalf("exit = %d (%v) stderr %q", r.exit, r.err, r.stderr)
	}
	mu.Lock()
	defer mu.Unlock()
	if idPolls != 2 {
		t.Errorf("polled ownerids %d times, want 2 (until the replaced user is gone)", idPolls)
	}
	if !strings.Contains(r.stderr, "Owners provisioned on entitlement e-1 of app app-1") {
		t.Errorf("stderr = %q", r.stderr)
	}
}

// TestSetOwnersEntitlementFormWaitTimesOut pins that a v2 set whose user
// removal never reaches ownerids exits 1 with the applied wording, even when
// the v2 user list is already empty.
func TestSetOwnersEntitlementFormWaitTimesOut(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, withLiveRefs(func(r *http.Request, _ int) mutReply {
		switch {
		case r.Method == http.MethodPut:
			return mutReply{body: `{}`}
		case strings.HasSuffix(r.URL.Path, "/ownerids"):
			return mutReply{body: `{"userIds":["u-old"]}`}
		}
		return mutReply{body: `{"list":[{"appEntitlement":{"id":"e-9","appId":"app-1"}}]}`}
	}))
	r := runMutationCLI(t, s.srv, 0, nil, "apps", "resources", "set-owners", "r-1", "--app-id", "app-1", "--resource-type-id", "rt-1", "--owner-entitlement-id", "e-9", "--no-user-owners", "--wait", "--wait-timeout", "100ms")
	if r.exit != 1 {
		t.Fatalf("exit = %d, want 1 (%v)", r.exit, r.err)
	}
	if r.err == nil || !strings.Contains(r.err.Error(), "was applied") {
		t.Errorf("error %v, want the applied wording", r.err)
	}
}

// TestSetOwnersEntitlementFormNeedsUserDecision pins that the v2 set, which
// also replaces user owners, needs --user-id or --no-user-owners: either
// refusal exits 2 before any request, --dry-run included.
func TestSetOwnersEntitlementFormNeedsUserDecision(t *testing.T) {
	const keepUsers = "--owner-entitlement-id also replaces user owners: repeat --user-id for each user owner to keep, or pass --no-user-owners to remove them all; to add one entitlement owner use add-owner"
	for _, f := range entOwnerFamilies {
		for _, dry := range []bool{false, true} {
			cases := []struct {
				extra []string
				want  string // "" = accepted
			}{
				{[]string{"--owner-entitlement-id", "e-9"}, keepUsers},
				{[]string{"--user-id", "u1", "--no-user-owners"}, "--no-user-owners requires --owner-entitlement-id"},
				{[]string{"--no-user-owners"}, "--no-user-owners requires --owner-entitlement-id"},
				{[]string{"--owner-entitlement-id", "e-9", "--user-id", "u1", "--no-user-owners"}, "--no-user-owners and --user-id are mutually exclusive"},
				{[]string{"--owner-entitlement-id", "e-9", "--no-user-owners"}, ""},
				{[]string{"--owner-entitlement-id", "e-9", "--user-id", "u1"}, ""},
			}
			for _, tc := range cases {
				name := f.name + " " + strings.Join(tc.extra, " ")
				if dry {
					name += " dry-run"
				}
				t.Run(name, func(t *testing.T) {
					t.Setenv("C1I_URL", mutTestURL)
					s := newMutServer(t, withLiveRefs(func(r *http.Request, _ int) mutReply {
						if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/users/") {
							return mutReply{body: `{"user":{"id":"u1"}}`}
						}
						return mutReply{body: `{}`}
					}))
					args := entOwnerArgs(f.prefix, "set-owners", f.args, tc.extra)
					if dry {
						args = append(args, "--dry-run")
					}
					r := runMutationCLI(t, s.srv, 0, nil, args...)
					if tc.want == "" {
						if r.exit != 0 {
							t.Fatalf("exit = %d (%v)", r.exit, r.err)
						}
						return
					}
					if r.exit != exitUsage || r.err == nil || r.err.Error() != tc.want {
						t.Errorf("exit %d, error %v; want exit %d, %q", r.exit, r.err, exitUsage, tc.want)
					}
					if n := len(s.requests()); n != 0 {
						t.Errorf("sent %d request(s); a usage error sends nothing", n)
					}
				})
			}
		}
	}
}

// TestAddEntitlementOwnerRefusesWait pins that --wait is a usage error with
// --owner-entitlement-id: nothing is sent.
func TestAddEntitlementOwnerRefusesWait(t *testing.T) {
	for _, f := range entOwnerFamilies[1:] {
		t.Run(f.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, func(*http.Request, int) mutReply { return mutReply{body: `{}`} })
			r := runMutationCLI(t, s.srv, 0, nil, entOwnerArgs(f.prefix, "add-owner", f.args, []string{"--owner-entitlement-id", "e-9", "--wait", "--wait-timeout", "1s"})...)
			if r.exit != exitUsage || r.err == nil || !strings.Contains(r.err.Error(), "--wait does not apply with --owner-entitlement-id") {
				t.Errorf("exit %d, error %v; want the --wait refusal", r.exit, r.err)
			}
			if n := len(s.requests()); n != 0 {
				t.Errorf("sent %d request(s); a usage error sends nothing", n)
			}
		})
	}
}
