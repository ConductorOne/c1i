package cmd

import (
	"net/http"
	"strings"
	"testing"
)

const (
	tblXAARS         = `{"resourceServer":{"id":"rs-1","appId":"app-1","displayName":"zz"}}`
	tblXAARSDoc      = `{"id":"rs-1","appId":"app-1","displayName":"zz"}`
	tblXAAScope      = `{"scope":{"id":"sc-1","appId":"app-1","scopeValue":"zz.read"}}`
	tblXAAScopeDoc   = `{"id":"sc-1","appId":"app-1","scopeValue":"zz.read"}`
	tblXAAProfile    = `{"accessProfile":{"id":"ap-1","appId":"app-1","displayName":"zz"}}`
	tblXAAProfileDoc = `{"id":"ap-1","appId":"app-1","displayName":"zz"}`
)

const xaaRSRead = `{"resourceServer":{"id":"rs-1","displayName":"cur","normalizedAudience":"https://a.example.com","deletedAt":null}}`

// xaaRoute answers GETs from reads (keyed by path suffix, 404 otherwise) and
// every write with write.
func xaaRoute(reads map[string]string, write string) func(*http.Request, int) mutReply {
	return func(r *http.Request, _ int) mutReply {
		if r.Method == http.MethodGet {
			for suffix, body := range reads {
				if strings.HasSuffix(r.URL.Path, suffix) {
					return mutReply{body: body}
				}
			}
			return mutReply{status: http.StatusNotFound, body: `{"code":5,"message":"not found"}`}
		}
		return mutReply{body: write}
	}
}

// TestXAACreatesCheckReferencesFirst: the server stores a create under an
// unknown app or an unknown or deleted resource server, so c1i looks them up
// first: unknown exits 4, deleted exits 2, nothing written, dry run included.
func TestXAACreatesCheckReferencesFirst(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	deleted := `{"resourceServer":{"id":"rs-1","deletedAt":"2026-01-01T00:00:00Z"}}`
	cases := []struct {
		name  string
		args  []string
		reads map[string]string
		exit  int
		want  string
	}{
		{"rs create unknown app", []string{"xaa", "resource-servers", "create", "--app-id", "app-x", "--audience", "https://a.example.com", "--display-name", "zz"}, nil, exitNotFound, `--app-id "app-x": no such app`},
		{"scope create unknown rs", []string{"xaa", "scopes", "create", "--app-id", "app-1", "--resource-server-id", "rs-x", "--scope-value", "s", "--display-name", "zz"}, nil, exitNotFound, `--resource-server-id "rs-x": no such resource server`},
		{"scope create deleted rs", []string{"xaa", "scopes", "create", "--app-id", "app-1", "--resource-server-id", "rs-1", "--scope-value", "s", "--display-name", "zz"}, map[string]string{"/rs-1": deleted}, exitUsage, "the resource server is deleted"},
		{"profile create unknown rs", []string{"xaa", "access-profiles", "create", "--app-id", "app-1", "--resource-server-id", "rs-x", "--display-name", "zz"}, nil, exitNotFound, `--resource-server-id "rs-x": no such resource server`},
		{"profile create deleted rs", []string{"xaa", "access-profiles", "create", "--app-id", "app-1", "--resource-server-id", "rs-1", "--display-name", "zz"}, map[string]string{"/rs-1": deleted}, exitUsage, "the resource server is deleted"},
	}
	for _, tc := range cases {
		for _, dry := range []bool{false, true} {
			t.Run(tc.name, func(t *testing.T) {
				clearViperOverrides(t, "dry_run")
				s := newMutServer(t, xaaRoute(tc.reads, `{}`))
				args := tc.args
				if dry {
					args = append(append([]string{}, args...), "--dry-run")
				}
				r := runMutationCLI(t, s.srv, 0, nil, args...)
				if r.exit != tc.exit || !strings.Contains(r.stderr, tc.want) {
					t.Errorf("dry=%v: exit %d stderr %q, want %d and %q", dry, r.exit, r.stderr, tc.exit, tc.want)
				}
				if w := writes(s); len(w) != 0 {
					t.Errorf("dry=%v: sent %v", dry, w)
				}
			})
		}
	}
}

// TestXAAUpdatesFillServerValidatedFields: the server validates fields the
// mask does not name, so an update fills them from a read; the mask is the
// flags' alone.
func TestXAAUpdatesFillServerValidatedFields(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	clearViperOverrides(t, "dry_run")
	cases := []struct {
		name, read string
		args       []string
		path, body string
	}{
		{"resource server", xaaRSRead, []string{"xaa", "resource-servers", "update", "rs-1", "--app-id", "app-1", "--description", "", "--disabled", "--resource-uri", "https://a.example.com/api", "--max-grant-lifetime", "1h", "--signing-algorithm", "es256"},
			"/api/v1/apps/app-1/xaa/resource_servers/rs-1",
			`{"resourceServer":{"description":"","disabled":true,"displayName":"cur","maxGrantLifetime":"3600s","normalizedAudience":"https://a.example.com","resourceUris":["https://a.example.com/api"],"signingAlgorithm":"XAA_SIGNING_ALGORITHM_ES256"},"updateMask":"description,resourceUris,maxGrantLifetime,signingAlgorithm,disabled"}`},
		{"scope", `{"scope":{"id":"sc-1","displayName":"cur","scopeValue":"zz.read","xaaResourceServerId":"rs-1"}}`, []string{"xaa", "scopes", "update", "sc-1", "--app-id", "app-1", "--state", "enabled", "--classification", "read"},
			"/api/v1/apps/app-1/xaa/scopes/sc-1",
			`{"scope":{"classification":"XAA_SCOPE_CLASSIFICATION_READ","displayName":"cur","scopeValue":"zz.read","state":"XAA_SCOPE_STATE_ENABLED","xaaResourceServerId":"rs-1"},"updateMask":"state,classification"}`},
		{"access profile", `{"accessProfile":{"id":"ap-1","displayName":"cur","xaaResourceServerId":"rs-1"}}`, []string{"xaa", "access-profiles", "update", "ap-1", "--app-id", "app-1", "--display-name", "new"},
			"/api/v1/apps/app-1/xaa/access_profiles/ap-1",
			`{"accessProfile":{"displayName":"new","xaaResourceServerId":"rs-1"},"updateMask":"displayName"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newMutServer(t, func(r *http.Request, _ int) mutReply {
				if r.Method == http.MethodGet {
					return mutReply{body: tc.read}
				}
				return mutReply{body: `{}`}
			})
			r := runMutationCLI(t, s.srv, 0, nil, tc.args...)
			if r.err != nil {
				t.Fatalf("exit %d: %v\n%s", r.exit, r.err, r.stderr)
			}
			w := writes(s)
			if len(w) != 1 || w[0].rawPath != tc.path {
				t.Fatalf("writes = %v, want one to %s", w, tc.path)
			}
			if got := sortedJSON(t, w[0].body); got != tc.body {
				t.Errorf("body\n got %s\nwant %s", got, tc.body)
			}
		})
	}
}

// TestXAAUpdateUnknownIDExits4BeforeSend: the fill's read 404s first.
func TestXAAUpdateUnknownIDExits4BeforeSend(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	for _, dry := range []bool{false, true} {
		clearViperOverrides(t, "dry_run")
		s := newMutServer(t, xaaRoute(nil, `{}`))
		args := []string{"xaa", "scopes", "update", "sc-x", "--app-id", "app-1", "--description", "d"}
		if dry {
			args = append(args, "--dry-run")
		}
		r := runMutationCLI(t, s.srv, 0, nil, args...)
		if r.exit != exitNotFound || len(writes(s)) != 0 {
			t.Errorf("dry=%v: exit %d, writes %v; want 4 and none", dry, r.exit, writes(s))
		}
	}
}

// TestXAAResourceServerBodyFileUpdate: a body that sets both validated fields
// needs no fill read, and a mask naming one the body leaves out is refused.
func TestXAAResourceServerBodyFileUpdate(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	clearViperOverrides(t, "dry_run")
	base := []string{"xaa", "resource-servers", "update", "rs-1", "--app-id", "app-1"}

	s := newMutServer(t, xaaRoute(map[string]string{"/functions/fn-1": `{"function":{"id":"fn-1"}}`}, `{}`))
	full := `{"displayName":"n","normalizedAudience":"https://a.example.com","modifyClaimsHook":{"functionId":"fn-1"}}`
	r := runMutationCLI(t, s.srv, 0, nil, append(base, "--body-file", writeTempFile(t, full), "--update-mask", "modifyClaimsHook")...)
	if r.err != nil {
		t.Fatalf("exit %d: %v\n%s", r.exit, r.err, r.stderr)
	}
	if sent := s.requests(); len(sent) != 2 || sent[0].rawPath != "/api/v1/functions/fn-1" || sent[1].method != http.MethodPost {
		t.Errorf("requests = %+v, want the function lookup and the update, no fill read", sent)
	}

	s = newMutServer(t, xaaRoute(map[string]string{"/rs-1": xaaRSRead}, `{}`))
	r = runMutationCLI(t, s.srv, 0, nil, append(base, "--body-file", writeTempFile(t, `{"description":"d"}`), "--update-mask", "display_name")...)
	if r.exit != exitUsage || len(s.requests()) != 0 {
		t.Errorf("exit %d (%v), %d request(s); want 2, none", r.exit, r.err, len(s.requests()))
	}
}

// TestXAAScopeStateRemovedIsRefused: create refuses REMOVED server-side but
// update accepts it, so c1i refuses it on both before any request.
func TestXAAScopeStateRemovedIsRefused(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	clearViperOverrides(t, "dry_run")
	for _, args := range [][]string{
		{"xaa", "scopes", "update", "sc-1", "--app-id", "app-1", "--state", "removed"},
		{"xaa", "scopes", "create", "--app-id", "app-1", "--resource-server-id", "rs-1", "--scope-value", "s", "--display-name", "zz", "--state", "XAA_SCOPE_STATE_REMOVED"},
	} {
		s := newMutServer(t, xaaRoute(map[string]string{"/rs-1": xaaRSRead}, `{}`))
		r := runMutationCLI(t, s.srv, 0, nil, args...)
		if r.exit != exitUsage || len(s.requests()) != 0 || !strings.Contains(r.stderr, "--state removed") {
			t.Errorf("%v: exit %d stderr %q, %d request(s); want 2, none", args, r.exit, r.stderr, len(s.requests()))
		}
	}
}

// TestXAAResourceServerSearchBody: --disabled is tri-state (omitted lists
// both), and the filters map onto the search body.
func TestXAAResourceServerSearchBody(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	cases := []struct {
		args []string
		body string
	}{
		{nil, `{"pageSize":50}`},
		{[]string{"--disabled=false"}, `{"disabled":false,"pageSize":50}`},
		{[]string{"--disabled", "--app-id", "a1", "--app-id", "a2", "--query", "zz", "--signing-algorithm", "unspecified", "--signing-algorithm", "rs256"},
			`{"appIds":["a1","a2"],"disabled":true,"pageSize":50,"query":"zz","signingAlgorithmFilter":["XAA_SIGNING_ALGORITHM_UNSPECIFIED","XAA_SIGNING_ALGORITHM_RS256"]}`},
	}
	for _, tc := range cases {
		s := newMutServer(t, fixedReply(http.StatusOK, `{"list":[],"nextPageToken":""}`))
		stubResourcesListClient(t, s.srv)
		r := runMutationCLI(t, s.srv, 0, nil, append([]string{"xaa", "resource-servers", "search"}, tc.args...)...)
		if r.err != nil {
			t.Fatalf("%v: exit %d: %v", tc.args, r.exit, r.err)
		}
		sent := s.requests()
		if len(sent) != 1 || sent[0].rawPath != "/api/v1/search/xaa/resource_servers" {
			t.Fatalf("%v: requests %+v", tc.args, sent)
		}
		if got := sortedJSON(t, sent[0].body); got != tc.body {
			t.Errorf("%v: body\n got %s\nwant %s", tc.args, got, tc.body)
		}
	}
}

// TestXAARowsKeepJSONTypes: bools stay bools, a number stays a number, and an
// empty string or list is null.
func TestXAARowsKeepJSONTypes(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	cases := []struct {
		args       []string
		page, want string
	}{
		{[]string{"xaa", "resource-servers", "list", "--app-id", "app-1"},
			`{"resourceServers":[{"id":"rs-1","appId":"app-1","displayName":"zz","description":"","normalizedAudience":"https://a.example.com","resourceUris":[],"maxGrantLifetime":null,"signingAlgorithm":"XAA_SIGNING_ALGORITHM_UNSPECIFIED","requireProofOfPossession":false,"modifyClaimsHook":null,"disabled":true,"sectorId":"","createdAt":"c","updatedAt":"u"}],"nextPageToken":""}`,
			`{"app_id":"app-1","created_at":"c","description":null,"disabled":true,"display_name":"zz","id":"rs-1","max_grant_lifetime":null,"modify_claims_hook":null,"normalized_audience":"https://a.example.com","require_proof_of_possession":false,"resource_uris":null,"sector_id":null,"signing_algorithm":"XAA_SIGNING_ALGORITHM_UNSPECIFIED","updated_at":"u"}`},
		{[]string{"xaa", "scopes", "list", "--app-id", "app-1"},
			`{"scopes":[{"id":"sc-1","appId":"app-1","xaaResourceServerId":"rs-1","scopeValue":"zz.read","displayName":"zz","description":"","state":"XAA_SCOPE_STATE_ENABLED","classification":"XAA_SCOPE_CLASSIFICATION_UNSPECIFIED","source":"XAA_SCOPE_SOURCE_ADMIN_DECLARED","appEntitlementId":"e-1","lastDiscoveredAt":null,"createdAt":"c","updatedAt":"u"}],"nextPageToken":""}`,
			`{"app_entitlement_id":"e-1","app_id":"app-1","classification":"XAA_SCOPE_CLASSIFICATION_UNSPECIFIED","created_at":"c","description":null,"display_name":"zz","id":"sc-1","last_discovered_at":null,"resource_server_id":"rs-1","scope_value":"zz.read","source":"XAA_SCOPE_SOURCE_ADMIN_DECLARED","state":"XAA_SCOPE_STATE_ENABLED","updated_at":"u"}`},
		{[]string{"xaa", "access-profiles", "list", "--app-id", "app-1"},
			`{"accessProfiles":[{"id":"ap-1","appId":"app-1","xaaResourceServerId":"rs-1","displayName":"zz","description":"d","appEntitlementId":"e-1","scopeCount":2,"createdAt":"c","updatedAt":"u"}],"nextPageToken":""}`,
			`{"app_entitlement_id":"e-1","app_id":"app-1","created_at":"c","description":"d","display_name":"zz","id":"ap-1","resource_server_id":"rs-1","scope_count":2,"updated_at":"u"}`},
	}
	for _, tc := range cases {
		s := newMutServer(t, fixedReply(http.StatusOK, tc.page))
		stubResourcesListClient(t, s.srv)
		r := runMutationCLI(t, s.srv, 0, nil, tc.args...)
		if r.err != nil {
			t.Fatalf("%v: exit %d: %v", tc.args, r.exit, r.err)
		}
		if got := strings.TrimSpace(r.stdout); got != tc.want {
			t.Errorf("%v: row\n got %s\nwant %s", tc.args, got, tc.want)
		}
	}
}

// TestXAAReadPaths pins the routes the reads send.
func TestXAAReadPaths(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	cases := []struct {
		args       []string
		path, body string
	}{
		{[]string{"xaa", "resource-servers", "history", "rs-1", "--app-id", "app-1"}, "/api/v1/apps/app-1/xaa/resource_servers/rs-1/history", `{"list":[],"nextPageToken":""}`},
		{[]string{"xaa", "scopes", "history", "sc-1", "--app-id", "app-1"}, "/api/v1/apps/app-1/xaa/scopes/sc-1/history", `{"list":[],"nextPageToken":""}`},
		{[]string{"xaa", "access-profiles", "history", "ap-1", "--app-id", "app-1"}, "/api/v1/apps/app-1/xaa/access_profiles/ap-1/history", `{"list":[],"nextPageToken":""}`},
		{[]string{"xaa", "access-profiles", "for-entitlement", "e-1", "--app-id", "app-1"}, "/api/v1/apps/app-1/xaa/access_profiles/by_app_entitlement_id/e-1", tblXAAProfile},
	}
	for _, tc := range cases {
		s := newMutServer(t, fixedReply(http.StatusOK, tc.body))
		stubResourcesListClient(t, s.srv)
		r := runMutationCLI(t, s.srv, 0, nil, tc.args...)
		if r.err != nil {
			t.Fatalf("%v: exit %d: %v", tc.args, r.exit, r.err)
		}
		if sent := s.requests(); len(sent) != 1 || sent[0].rawPath != tc.path {
			t.Errorf("%v: requests %+v, want GET %s", tc.args, sent, tc.path)
		}
	}
}

// TestXAAUpdateMatchesSnakeCaseBodyKeys: a snake_case body key counts as set,
// so the fill does not add its camelCase twin and a mask naming it is not
// refused.
func TestXAAUpdateMatchesSnakeCaseBodyKeys(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	clearViperOverrides(t, "dry_run")
	base := []string{"xaa", "resource-servers", "update", "rs-1", "--app-id", "app-1"}
	cases := []struct {
		name, file, mask, body string
		reads                  int
	}{
		{"fill skips a snake_case key", `{"display_name":"n"}`, "description",
			`{"resourceServer":{"display_name":"n","normalizedAudience":"https://a.example.com"},"updateMask":"description"}`, 1},
		{"mask names a snake_case key the body sets", `{"display_name":"n","normalized_audience":"https://a.example.com"}`, "display_name",
			`{"resourceServer":{"display_name":"n","normalized_audience":"https://a.example.com"},"updateMask":"display_name"}`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newMutServer(t, xaaRoute(map[string]string{"/rs-1": xaaRSRead}, `{}`))
			r := runMutationCLI(t, s.srv, 0, nil, append(base, "--body-file", writeTempFile(t, tc.file), "--update-mask", tc.mask)...)
			if r.err != nil {
				t.Fatalf("exit %d: %v\n%s", r.exit, r.err, r.stderr)
			}
			w := writes(s)
			if len(w) != 1 || len(s.requests()) != tc.reads+1 {
				t.Fatalf("requests = %+v, want %d read(s) and one write", s.requests(), tc.reads)
			}
			if got := sortedJSON(t, w[0].body); got != tc.body {
				t.Errorf("body\n got %s\nwant %s", got, tc.body)
			}
		})
	}
}

// TestXAAClaimsHookFunctionIsLookedUp: the server stores a hook naming an
// unknown function or commit, so c1i looks both up first (exit 4, nothing
// written, dry run included), in either spelling.
func TestXAAClaimsHookFunctionIsLookedUp(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	known := map[string]string{"/rs-1": xaaRSRead, "/apps/app-1": `{"app":{"id":"app-1"}}`, "/functions/fn-1": `{"function":{"id":"fn-1"}}`, "/functions/fn-1/commits/c-1": `{"commit":{"id":"c-1"}}`}
	create := []string{"xaa", "resource-servers", "create", "--app-id", "app-1", "--body-file"}
	update := []string{"xaa", "resource-servers", "update", "rs-1", "--app-id", "app-1", "--update-mask", "modifyClaimsHook", "--body-file"}
	cases := []struct {
		name string
		args []string
		file string
		exit int
		want string
	}{
		{"create unknown function", create, `{"displayName":"n","normalizedAudience":"https://a.example.com","modifyClaimsHook":{"functionId":"fn-x"}}`, exitNotFound, `modifyClaimsHook.functionId "fn-x": no such function`},
		{"create snake_case unknown commit", create, `{"display_name":"n","normalized_audience":"https://a.example.com","modify_claims_hook":{"function_id":"fn-1","commit_id":"c-x"}}`, exitNotFound, `modifyClaimsHook.commitId "c-x": no such commit of function "fn-1"`},
		{"update unknown function", update, `{"modifyClaimsHook":{"functionId":"fn-x"}}`, exitNotFound, `modifyClaimsHook.functionId "fn-x": no such function`},
		{"update known function and commit", update, `{"modifyClaimsHook":{"functionId":"fn-1","commitId":"c-1"}}`, 0, ""},
		{"update clears the hook", update, `{}`, 0, ""},
	}
	for _, tc := range cases {
		for _, dry := range []bool{false, true} {
			t.Run(tc.name, func(t *testing.T) {
				clearViperOverrides(t, "dry_run")
				s := newMutServer(t, xaaRoute(known, `{}`))
				args := append(append([]string{}, tc.args...), writeTempFile(t, tc.file))
				if dry {
					args = append(args, "--dry-run")
				}
				r := runMutationCLI(t, s.srv, 0, nil, args...)
				if r.exit != tc.exit || !strings.Contains(r.stderr, tc.want) {
					t.Fatalf("dry=%v: exit %d stderr %q, want %d and %q", dry, r.exit, r.stderr, tc.exit, tc.want)
				}
				if w := writes(s); tc.exit != 0 && len(w) != 0 {
					t.Errorf("dry=%v: sent %v", dry, w)
				}
			})
		}
	}
}
