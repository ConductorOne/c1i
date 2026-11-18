package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// stubResourcesListClient points list commands (newListClient) at srv.
func stubResourcesListClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	orig := newListClient
	newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newListClient = orig })
}

var resScopeArgs = []string{"--app-id", "app-1", "--resource-type-id", "rt-1"}

// writes returns the non-GET requests s saw.
func writes(s *mutServer) []entOwnerRequest {
	var out []entOwnerRequest
	for _, r := range s.requests() {
		if r.method != http.MethodGet {
			out = append(out, r)
		}
	}
	return out
}

// TestResourceSetOwnersRefusesUnknownResource: the API answers a set-owners on
// an unknown resource 200 and stores the owners, so the command must read the
// resource first and exit 4 without the PUT, on a dry run too.
func TestResourceSetOwnersRefusesUnknownResource(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	for _, dry := range []bool{false, true} {
		clearViperOverrides(t, "dry_run")
		s := newMutServer(t, func(r *http.Request, _ int) mutReply {
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/v1/apps/app-1":
				return mutReply{body: `{"app":{"id":"app-1"}}`}
			case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/users/"):
				return mutReply{body: `{"user":{"id":"u1"}}`}
			case r.Method == http.MethodGet:
				return mutReply{status: http.StatusNotFound, body: `{"code":5,"message":"not found"}`}
			}
			return mutReply{body: `{}`}
		})
		args := append([]string{"apps", "resources", "set-owners", "r-unknown", "--user-id", "u1"}, resScopeArgs...)
		if dry {
			args = append(args, "--dry-run")
		}
		r := runMutationCLI(t, s.srv, 0, nil, args...)
		if r.exit != exitNotFound {
			t.Errorf("dry=%v: exit %d, want %d (stderr %q)", dry, r.exit, exitNotFound, r.stderr)
		}
		if !strings.Contains(r.stderr, `"r-unknown"`) {
			t.Errorf("dry=%v: stderr %q does not name the resource", dry, r.stderr)
		}
		if w := writes(s); len(w) != 0 {
			t.Errorf("dry=%v: sent %v despite an unknown resource", dry, w)
		}
		var sawRead bool
		for _, rq := range s.requests() {
			sawRead = sawRead || (rq.method == http.MethodGet && rq.rawPath == "/api/v1/apps/app-1/resource_types/rt-1/resources/r-unknown")
		}
		if !sawRead {
			t.Errorf("dry=%v: no GET of the resource: %v", dry, s.requests())
		}
	}
}

// TestResourceCreateChecksResourceType: a deleted type exits 2 and an unknown
// one 4, before the create, on a dry run too; an unrecognized lookup exits 6.
func TestResourceCreateChecksResourceType(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	cases := []struct {
		name   string
		status int
		body   string
		exit   int
		stderr string
	}{
		{"deleted", http.StatusOK, `{"appResourceTypeView":{"appResourceType":{"id":"rt-1","deletedAt":"2026-01-01T00:00:00Z"}}}`, exitUsage, "is deleted"},
		{"unknown", http.StatusNotFound, `{"code":5,"message":"not found"}`, exitNotFound, "no such resource type"},
		{"unrecognized", http.StatusOK, `{}`, exitServer, "did not return a resource type"},
	}
	for _, tc := range cases {
		for _, dry := range []bool{false, true} {
			clearViperOverrides(t, "dry_run")
			s := newMutServer(t, func(r *http.Request, _ int) mutReply {
				if r.Method == http.MethodGet {
					return mutReply{status: tc.status, body: tc.body}
				}
				return mutReply{body: `{"appResource":{"id":"r-1"}}`}
			})
			args := append([]string{"apps", "resources", "create", "--display-name", "zz"}, resScopeArgs...)
			if dry {
				args = append(args, "--dry-run")
			}
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.exit != tc.exit || !strings.Contains(r.stderr, tc.stderr) {
				t.Errorf("%s dry=%v: exit %d stderr %q, want exit %d mentioning %q", tc.name, dry, r.exit, r.stderr, tc.exit, tc.stderr)
			}
			if w := writes(s); len(w) != 0 {
				t.Errorf("%s dry=%v: sent %v", tc.name, dry, w)
			}
		}
	}
}

// TestResourceCommandsWire pins each write's method, path and body.
func TestResourceCommandsWire(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	clearViperOverrides(t, "dry_run")
	owners := "/api/v1/apps/app-1/resource_types/rt-1/resource/r-1/owners"
	cases := []struct {
		name               string
		args               []string
		method, path, body string
	}{
		{"type create", []string{"apps", "resource-types", "create", "--app-id", "app-1", "--display-name", "zz", "--resource-type", "custom"},
			"POST", "/api/v1/apps/app-1/resource_types", `{"displayName":"zz","resourceType":"CUSTOM"}`},
		{"type update", []string{"apps", "resource-types", "update", "rt-1", "--app-id", "app-1", "--display-name", "zz"},
			"POST", "/api/v1/apps/app-1/resource_types/rt-1", `{"appResourceType":{"displayName":"zz"},"updateMask":"displayName"}`},
		{"create", append([]string{"apps", "resources", "create", "--display-name", "zz", "--description", "d", "--owner-id", "u1", "--owner-id", "u1"}, resScopeArgs...),
			"POST", "/api/v1/apps/app-1/resource_types/rt-1/resources", `{"description":"d","displayName":"zz","resourceOwnerUserIds":["u1"]}`},
		{"update clears custom description", append([]string{"apps", "resources", "update", "r-1", "--display-name", "zz", "--custom-description", ""}, resScopeArgs...),
			"POST", "/api/v1/apps/app-1/resource_types/rt-1/resources/r-1", `{"appResource":{"customDescription":"","displayName":"zz"},"updateMask":"displayName,customDescription"}`},
		{"update body file", append([]string{"apps", "resources", "update", "r-1", "--body-file", writeTempFile(t, `{"annotations":{"k":"v"}}`), "--update-mask", "annotations"}, resScopeArgs...),
			"POST", "/api/v1/apps/app-1/resource_types/rt-1/resources/r-1", `{"appResource":{"annotations":{"k":"v"}},"updateMask":"annotations"}`},
		{"add-owner", append([]string{"apps", "resources", "add-owner", "r-1", "--user-id", "u1"}, resScopeArgs...), "POST", owners, `{"userId":"u1"}`},
		{"remove-owner", append([]string{"apps", "resources", "remove-owner", "r-1", "--user-id", "u1"}, resScopeArgs...), "DELETE", owners, `{"userId":"u1"}`},
		{"set-owners", append([]string{"apps", "resources", "set-owners", "r-1", "--user-id", "u1", "--user-id", "u2", "--user-id", "u1"}, resScopeArgs...), "PUT", owners, `{"userIds":["u1","u2"]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newMutServer(t, func(r *http.Request, _ int) mutReply {
				if r.Method == http.MethodGet {
					return mutReply{body: `{"user":{"id":"u1"},"app":{"id":"app-1"},"appResourceTypeView":{"appResourceType":{"id":"rt-1"}},"appResourceView":{"appResource":{"id":"r-1"}}}`}
				}
				return mutReply{body: `{}`}
			})
			r := runMutationCLI(t, s.srv, 0, nil, tc.args...)
			if r.err != nil {
				t.Fatalf("exit %d: %v\n%s", r.exit, r.err, r.stderr)
			}
			w := writes(s)
			if len(w) != 1 {
				t.Fatalf("writes = %v, want one", w)
			}
			if w[0].method != tc.method || w[0].rawPath != tc.path {
				t.Errorf("sent %s %s, want %s %s", w[0].method, w[0].rawPath, tc.method, tc.path)
			}
			if got := sortedJSON(t, w[0].body); got != tc.body {
				t.Errorf("body %s, want %s", got, tc.body)
			}
		})
	}
}

// sortedJSON re-encodes a JSON text with sorted keys.
func sortedJSON(t *testing.T, s string) string {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("body %q: %v", s, err)
	}
	return compactJSON(t, v)
}

// TestResourceSearchKeepsRowsSharingAnIDAcrossApps: a resource type or
// resource id repeats across apps, so the cursor guard must key search rows on
// the app (and type), not drop a later page's row for another app as a repeat.
func TestResourceSearchKeepsRowsSharingAnIDAcrossApps(t *testing.T) {
	cases := []struct {
		args         []string
		page1, page2 string
	}{
		{[]string{"apps", "resource-types", "search"},
			`{"list":[{"id":"rt-1","appId":"app-1"}],"nextPageToken":"t1"}`,
			`{"list":[{"id":"rt-1","appId":"app-2"}],"nextPageToken":""}`},
		{[]string{"apps", "resources", "search"},
			`{"list":[{"appResource":{"id":"r-1","appId":"app-1","appResourceTypeId":"rt-1"}}],"nextPageToken":"t1"}`,
			`{"list":[{"appResource":{"id":"r-1","appId":"app-2","appResourceTypeId":"rt-1"}}],"nextPageToken":""}`},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, func(_ *http.Request, n int) mutReply {
				if n == 1 {
					return mutReply{body: tc.page1}
				}
				return mutReply{body: tc.page2}
			})
			stubResourcesListClient(t, s.srv)
			r := runMutationCLI(t, s.srv, 0, nil, tc.args...)
			if r.err != nil {
				t.Fatalf("exit %d: %v\n%s", r.exit, r.err, r.stderr)
			}
			rows := decodeNDJSONRows(t, r.stdout)
			if len(rows) != 2 || rows[0]["app_id"] != "app-1" || rows[1]["app_id"] != "app-2" {
				t.Errorf("rows = %v, want one per app (stderr %q)", rows, r.stderr)
			}
		})
	}
}

// TestResourceRowsKeepJSONTypes: grant_count is a number and
// is_manually_managed a bool; absent strings are null.
func TestResourceRowsKeepJSONTypes(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, fixedReply(http.StatusOK, `{"list":[{"appResource":{"id":"r-1","appId":"app-1","appResourceTypeId":"rt-1","displayName":"zz","grantCount":"24","isManuallyManaged":true,"deletedAt":null,"description":""}}],"nextPageToken":""}`))
	stubResourcesListClient(t, s.srv)
	r := runMutationCLI(t, s.srv, 0, nil, append([]string{"apps", "resources", "list"}, resScopeArgs...)...)
	if r.err != nil {
		t.Fatalf("exit %d: %v\n%s", r.exit, r.err, r.stderr)
	}
	want := `{"app_id":"app-1","custom_description":null,"description":null,"display_name":"zz","external_id":null,"grant_count":24,"id":"r-1","is_manually_managed":true,"parent_resource_id":null,"parent_resource_type_id":null,"resource_type_id":"rt-1"}`
	if got := strings.TrimSpace(r.stdout); got != want {
		t.Errorf("row\n got %s\nwant %s", got, want)
	}
	if got := s.requests()[0].rawPath; got != "/api/v1/apps/app-1/resource_types/rt-1/resources" {
		t.Errorf("path %s", got)
	}
}

// The create path's --app-id owns the app; a --body-file that also sets it is
// refused (the server ignores the body value, so sending it is misleading).
func TestResourceCreateBodyFileRefusesOwnedAppID(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	for _, body := range []string{`{"displayName":"x","appId":"a1"}`, `{"display_name":"x","app_id":"a1"}`} {
		s := newMutServer(t, xaaRoute(map[string]string{"/rt-1": `{"appResourceTypeView":{"appResourceType":{"id":"rt-1"}}}`, "/apps/app-1": `{"app":{"id":"app-1"}}`}, `{"appResource":{"id":"r1"}}`))
		r := runMutationCLI(t, s.srv, 0, nil, append([]string{"apps", "resources", "create"}, append(resScopeArgs, "--body-file", writeTempFile(t, body))...)...)
		if r.exit != exitUsage || !strings.Contains(r.stderr, "--app-id") {
			t.Errorf("body %s: exit %d stderr %q", body, r.exit, r.stderr)
		}
		if len(writes(s)) != 0 {
			t.Errorf("body %s: sent %v", body, writes(s))
		}
	}
}
