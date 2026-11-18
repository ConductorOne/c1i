package cmd

import (
	"net/http"
	"strings"
	"sync"
	"testing"
)

// TestXAAScopeBindingsCheckScopesFirst: the server answers an unknown scope
// with 400 on add and a silent 200 on remove, so each --scope-id is looked up
// first: unknown exits 4, deleted exits 2 on add only, nothing written, dry
// run included.
func TestXAAScopeBindingsCheckScopesFirst(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	reads := map[string]string{
		"/scopes/sc-1":   `{"scope":{"id":"sc-1"}}`,
		"/scopes/sc-del": `{"scope":{"id":"sc-del","deletedAt":"2026-01-01T00:00:00Z"}}`,
	}
	add := []string{"xaa", "access-profiles", "add-scopes", "ap-1", "--app-id", "app-1", "--scope-id", "sc-1"}
	remove := []string{"xaa", "access-profiles", "remove-scopes", "ap-1", "--app-id", "app-1", "--scope-id", "sc-1"}
	cases := []struct {
		name string
		args []string
		exit int
		want string
	}{
		{"add unknown", append(add, "--scope-id", "sc-x"), exitNotFound, `--scope-id "sc-x": no such scope under --app-id "app-1"`},
		{"add deleted", append(add, "--scope-id", "sc-del"), exitUsage, `--scope-id "sc-del": the scope is deleted`},
		{"remove unknown", append(remove, "--scope-id", "sc-x"), exitNotFound, `--scope-id "sc-x": no such scope under --app-id "app-1"`},
		{"remove deleted", append(remove, "--scope-id", "sc-del"), 0, ""},
	}
	for _, tc := range cases {
		for _, dry := range []bool{false, true} {
			t.Run(tc.name, func(t *testing.T) {
				clearViperOverrides(t, "dry_run")
				s := newMutServer(t, xaaRoute(reads, `{}`))
				args := tc.args
				if dry {
					args = append(append([]string{}, args...), "--dry-run")
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

// TestXAAScopeBindingsSendEachIDOnce: the server refuses a repeated id
// ("repeated value must contain unique items"), so a repeat is sent once.
func TestXAAScopeBindingsSendEachIDOnce(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	clearViperOverrides(t, "dry_run")
	s := newMutServer(t, xaaRoute(map[string]string{"/scopes/sc-1": `{"scope":{"id":"sc-1"}}`, "/scopes/sc-2": `{"scope":{"id":"sc-2"}}`}, `{"bindings":[]}`))
	r := runMutationCLI(t, s.srv, 0, nil, "xaa", "access-profiles", "add-scopes", "ap-1", "--app-id", "app-1", "--scope-id", "sc-1", "--scope-id", "sc-2", "--scope-id", "sc-1")
	if r.err != nil {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
	w := writes(s)
	if len(w) != 1 || w[0].rawPath != "/api/v1/apps/app-1/xaa/access_profiles/ap-1/scope_bindings" {
		t.Fatalf("writes = %+v", w)
	}
	if got := sortedJSON(t, w[0].body); got != `{"xaaScopeIds":["sc-1","sc-2"]}` {
		t.Errorf("body = %s", got)
	}
}

// queryRoute records each request's decoded client_key and answers like
// xaaRoute, with search and history POST/GET reads answered by list.
func queryRoute(t *testing.T, reads map[string]string, write string) (func(*http.Request, int) mutReply, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var keys []string
	inner := xaaRoute(reads, write)
	return func(r *http.Request, n int) mutReply {
			mu.Lock()
			keys = append(keys, r.URL.Query().Get("client_key"))
			mu.Unlock()
			return inner(r, n)
		}, func() []string {
			mu.Lock()
			defer mu.Unlock()
			return append([]string{}, keys...)
		}
}

// TestXAAClientKeyNeverTravelsInThePath: a client key may be a URL, so it goes
// in the query or body; one with "/", "?" and "#" must not reshape the path.
func TestXAAClientKeyNeverTravelsInThePath(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	clearViperOverrides(t, "dry_run")
	const key = "dcr://zz/a?b=1#c"
	base := "/api/v1/xaa/resource_servers/rs-1/client_audience_mappings"
	mapping := `{"clientAudienceMapping":{"xaaResourceServerId":"rs-1","clientKey":"dcr://zz/a?b=1#c","audienceClientId":"cur"}}`
	cases := []struct {
		args       []string
		path       string // the last request's path
		query      bool   // the key travels as ?client_key=
		body       string // the write's body, "" for a read
		requestsAt int
	}{
		{[]string{"get", key}, base + "/by_client_key", true, "", 1},
		{[]string{"history", key}, base + "/history", true, "", 1},
		{[]string{"delete", key}, base + "/delete", false, `{"clientKey":"dcr://zz/a?b=1#c"}`, 1},
		{[]string{"update", key, "--disabled"}, base + "/update", false, `{"clientAudienceMapping":{"audienceClientId":"cur","clientKey":"dcr://zz/a?b=1#c","disabled":true},"updateMask":"disabled"}`, 2},
		{[]string{"create", "--client-key", key, "--audience-client-id", "aud"}, base, false, `{"audienceClientId":"aud","clientKey":"dcr://zz/a?b=1#c"}`, 2},
	}
	for _, tc := range cases {
		t.Run(tc.args[0], func(t *testing.T) {
			route, keys := queryRoute(t, map[string]string{"/by_client_key": mapping, "/history": `{"list":[],"nextPageToken":""}`}, mapping)
			// create's resource-server search is a POST; answer it with rs-1.
			s := newMutServer(t, func(r *http.Request, n int) mutReply {
				if r.URL.Path == xaaResourceServersSearchPath {
					return mutReply{body: `{"list":[{"id":"rs-1"}],"nextPageToken":""}`}
				}
				return route(r, n)
			})
			stubResourcesListClient(t, s.srv)
			args := append([]string{"xaa", "client-audience-mappings"}, tc.args...)
			r := runMutationCLI(t, s.srv, 0, nil, append(args, "--resource-server-id", "rs-1")...)
			if r.err != nil {
				t.Fatalf("exit %d: %v\n%s", r.exit, r.err, r.stderr)
			}
			sent := s.requests()
			if len(sent) != tc.requestsAt {
				t.Fatalf("requests = %+v, want %d", sent, tc.requestsAt)
			}
			last := sent[len(sent)-1]
			if last.rawPath != tc.path {
				t.Errorf("path = %s, want %s", last.rawPath, tc.path)
			}
			k := keys()
			if got := k[len(k)-1]; tc.query != (got == key) {
				t.Errorf("client_key query = %q, want it %v", got, tc.query)
			}
			if tc.body != "" {
				if got := sortedJSON(t, last.body); got != tc.body {
					t.Errorf("body\n got %s\nwant %s", got, tc.body)
				}
			}
			if tc.args[0] == "update" && k[0] != key {
				t.Errorf("fill read client_key = %q, want %q", k[0], key)
			}
		})
	}
}

// TestXAAClientMappingUpdateSkipsTheFillWhenGiven: with --audience-client-id
// set, nothing is read; the mask is the flags' alone.
func TestXAAClientMappingUpdateSkipsTheFillWhenGiven(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	clearViperOverrides(t, "dry_run")
	s := newMutServer(t, xaaRoute(nil, tblXAAMapping))
	r := runMutationCLI(t, s.srv, 0, nil, "xaa", "client-audience-mappings", "update", "dcr://ck-1", "--resource-server-id", "rs-1", "--audience-client-id", "new")
	if r.err != nil {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
	if sent := s.requests(); len(sent) != 1 || sortedJSON(t, sent[0].body) != `{"clientAudienceMapping":{"audienceClientId":"new","clientKey":"dcr://ck-1"},"updateMask":"audienceClientId"}` {
		t.Errorf("requests = %+v", sent)
	}
}

// TestXAAClientMappingCreateSearchesForTheResourceServer: the server stores
// a mapping under an unknown resource server, and the routes carry no app, so
// create pages the tenant-wide search: a server on a later page passes, one
// on no page exits 4 with nothing written, dry run included.
func TestXAAClientMappingCreateSearchesForTheResourceServer(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	// The search answers page 1, then page 2 (the body is consumed by the stub).
	pages := func() func(*http.Request, int) mutReply {
		searched := 0
		return func(r *http.Request, _ int) mutReply {
			if r.URL.Path != xaaResourceServersSearchPath {
				return mutReply{body: tblXAAMapping}
			}
			searched++
			if searched > 1 {
				return mutReply{body: `{"list":[{"id":"rs-2"}],"nextPageToken":""}`}
			}
			return mutReply{body: `{"list":[{"id":"rs-1"}],"nextPageToken":"p2"}`}
		}
	}
	for _, tc := range []struct {
		rs   string
		exit int
		want string
	}{
		{"rs-2", 0, ""},
		{"rs-x", exitNotFound, `--resource-server-id "rs-x": resource server not found (or deleted)`},
	} {
		for _, dry := range []bool{false, true} {
			clearViperOverrides(t, "dry_run")
			s := newMutServer(t, pages())
			args := []string{"xaa", "client-audience-mappings", "create", "--resource-server-id", tc.rs, "--client-key", "dcr://ck-1", "--audience-client-id", "aud"}
			if dry {
				args = append(args, "--dry-run")
			}
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.exit != tc.exit || !strings.Contains(r.stderr, tc.want) {
				t.Errorf("%s dry=%v: exit %d stderr %q, want %d and %q", tc.rs, dry, r.exit, r.stderr, tc.exit, tc.want)
			}
			var tokens []string
			for _, q := range s.requests() {
				if q.rawPath == xaaResourceServersSearchPath {
					tokens = append(tokens, q.body)
				}
			}
			if len(tokens) != 2 || tokens[0] != `{"pageSize":100}` || !strings.Contains(tokens[1], `"pageToken":"p2"`) {
				t.Errorf("%s dry=%v: search bodies %q, want page 1 then the p2 cursor", tc.rs, dry, tokens)
			}
			if w := nonGETRequests(s); (tc.exit != 0 || dry) && len(w) != 0 {
				t.Errorf("%s dry=%v: sent %v", tc.rs, dry, w)
			}
		}
	}
}

// TestXAAPart2Reads pins the read routes, the search body and the rows' JSON
// types.
func TestXAAPart2Reads(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	cases := []struct {
		args             []string
		path, body, page string
		row              string
	}{
		{[]string{"xaa", "access-profiles", "scopes", "ap-1", "--app-id", "app-1"}, "/api/v1/apps/app-1/xaa/access_profiles/ap-1/scope_bindings", "",
			`{"bindings":[{"appId":"app-1","accessProfileId":"ap-1","xaaScopeId":"sc-1","xaaResourceServerId":"rs-1","createdAt":"c","updatedAt":"u","deletedAt":null}],"nextPageToken":""}`,
			`{"access_profile_id":"ap-1","app_id":"app-1","created_at":"c","resource_server_id":"rs-1","scope_id":"sc-1","updated_at":"u"}`},
		{[]string{"xaa", "client-audience-mappings", "list", "--resource-server-id", "rs-1"}, "/api/v1/xaa/resource_servers/rs-1/client_audience_mappings", "",
			`{"clientAudienceMappings":[{"xaaResourceServerId":"rs-1","clientKey":"dcr://ck-1","audienceClientId":"aud","disabled":false,"createdAt":"c","updatedAt":"u","deletedAt":null}],"nextPageToken":""}`,
			`{"audience_client_id":"aud","client_key":"dcr://ck-1","created_at":"c","disabled":false,"resource_server_id":"rs-1","updated_at":"u"}`},
		{[]string{"xaa", "client-audience-mappings", "search"}, xaaClientMappingsSearchPath, `{"pageSize":50}`, `{"list":[],"nextPageToken":""}`, ""},
		{[]string{"xaa", "client-audience-mappings", "search", "--resource-server-id", "rs-1", "--resource-server-id", "rs-2", "--query", "zz", "--disabled=false"}, xaaClientMappingsSearchPath,
			`{"disabled":false,"pageSize":50,"query":"zz","xaaResourceServerIds":["rs-1","rs-2"]}`,
			`{"list":[{"xaaResourceServerId":"rs-1","clientKey":"dcr://ck-1","audienceClientId":"aud","disabled":true}],"nextPageToken":""}`,
			`{"audience_client_id":"aud","client_key":"dcr://ck-1","created_at":null,"disabled":true,"resource_server_id":"rs-1","updated_at":null}`},
	}
	for _, tc := range cases {
		s := newMutServer(t, fixedReply(http.StatusOK, tc.page))
		stubResourcesListClient(t, s.srv)
		r := runMutationCLI(t, s.srv, 0, nil, tc.args...)
		if r.err != nil {
			t.Fatalf("%v: exit %d: %v", tc.args, r.exit, r.err)
		}
		sent := s.requests()
		if len(sent) != 1 || sent[0].rawPath != tc.path {
			t.Fatalf("%v: requests %+v, want %s", tc.args, sent, tc.path)
		}
		if tc.body != "" {
			if got := sortedJSON(t, sent[0].body); got != tc.body {
				t.Errorf("%v: body\n got %s\nwant %s", tc.args, got, tc.body)
			}
		}
		if got := strings.TrimSpace(r.stdout); got != tc.row {
			t.Errorf("%v: row\n got %s\nwant %s", tc.args, got, tc.row)
		}
	}
}
