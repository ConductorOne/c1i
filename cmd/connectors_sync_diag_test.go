package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const syncDiagBase = "/api/v1/apps/a1/connectors/cn1/sync_diagnostics/"

type syncDiagSeen struct{ method, path, query, body string }

// syncDiagServer records each request with its query and answers per route.
func syncDiagServer(t *testing.T, route func(r *http.Request, n int) mutReply) (*httptest.Server, func() []syncDiagSeen) {
	t.Helper()
	var mu sync.Mutex
	var seen []syncDiagSeen
	m := newMutServer(t, func(r *http.Request, n int) mutReply {
		mu.Lock()
		seen = append(seen, syncDiagSeen{r.Method, r.URL.EscapedPath(), r.URL.RawQuery, ""})
		mu.Unlock()
		return route(r, n)
	})
	return m.srv, func() []syncDiagSeen {
		mu.Lock()
		defer mu.Unlock()
		reqs := m.requests()
		out := append([]syncDiagSeen(nil), seen...)
		for i := range out {
			if i < len(reqs) {
				out[i].body = reqs[i].body
			}
		}
		return out
	}
}

func TestSyncDiagnosticsWire(t *testing.T) {
	for _, tc := range []struct {
		name       string
		args       []string
		method     string
		path       string
		query      string
		body       string
		wantStdout string
	}{
		{"history default", []string{"sync-history", "cn1", "--app-id", "a1"}, "GET", syncDiagBase + "history", "", "", `"runs"`},
		{"history runs", []string{"sync-history", "cn1", "--app-id", "a1", "--runs", "7"}, "GET", syncDiagBase + "history", "page_size=7", "", `"runs"`},
		{"history clamp", []string{"sync-history", "cn1", "--app-id", "a1", "--runs", "500"}, "GET", syncDiagBase + "history", "page_size=50", "", `"runs"`},
		{"errors default", []string{"sync-errors", "cn1", "--app-id", "a1"}, "GET", syncDiagBase + "error_digest", "", "", `"runs"`},
		{"errors top-k", []string{"sync-errors", "cn1", "--app-id", "a1", "--top-k", "3"}, "GET", syncDiagBase + "error_digest", "top_k=3", "", `"runs"`},
		{"errors clamp", []string{"sync-errors", "cn1", "--app-id", "a1", "--top-k", "99"}, "GET", syncDiagBase + "error_digest", "top_k=20", "", `"runs"`},
		{"changes default", []string{"sync-changes", "cn1", "--app-id", "a1"}, "POST", syncDiagBase + "change_timeline", "", `{}`, `"runs"`},
		{"changes since", []string{"sync-changes", "cn1", "--app-id", "a1", "--since", "2026-01-01T00:00:00Z"}, "POST", syncDiagBase + "change_timeline", "", `{"since":"2026-01-01T00:00:00Z"}`, `"runs"`},
		{"compare", []string{"sync-compare", "cn1", "--app-id", "a1", "--left", "L", "--right", "R"}, "POST", syncDiagBase + "compare_stats", "", `{"leftLifecycleId":"L","rightLifecycleId":"R"}`, `"runs"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			srv, seen := syncDiagServer(t, fixedReply(200, `{"runs":[],"streak":{}}`))
			r := runMutationCLI(t, srv, 0, nil, append([]string{"connectors"}, tc.args...)...)
			if r.exit != 0 {
				t.Fatalf("exit %d (%v)", r.exit, r.err)
			}
			if !strings.Contains(r.stdout, `"streak"`) {
				t.Errorf("stdout %q lost the response", r.stdout)
			}
			got := seen()
			if len(got) != 1 || got[0].method != tc.method || got[0].path != tc.path || got[0].query != tc.query {
				t.Fatalf("requests %+v, want %s %s ?%s", got, tc.method, tc.path, tc.query)
			}
			if tc.body != "" {
				assertJSONEqual(t, got[0].body, tc.body)
			}
		})
	}
}

func TestSyncDiagnosticsCompareBodyFile(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	dir := t.TempDir()
	file := filepath.Join(dir, "b.json")
	const body = `{"leftLifecycleId":"L","draftTestRun":{"catalogId":"c","revisionId":"r","testRunId":"t"}}`
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	srv, seen := syncDiagServer(t, fixedReply(200, `{}`))
	r := runMutationCLI(t, srv, 0, nil, "connectors", "sync-compare", "cn1", "--app-id", "a1", "--body-file", file)
	if r.exit != 0 {
		t.Fatalf("exit %d (%v)", r.exit, r.err)
	}
	got := seen()
	if len(got) != 1 || got[0].path != syncDiagBase+"compare_stats" {
		t.Fatalf("requests %+v", got)
	}
	assertJSONEqual(t, got[0].body, body)
}

func TestSyncDiagnosticsUsageErrorsSendNothing(t *testing.T) {
	file := filepath.Join(t.TempDir(), "n.json")
	if err := os.WriteFile(file, []byte(`null`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"history negative runs", []string{"sync-history", "cn1", "--app-id", "a1", "--runs", "-1"}},
		{"errors negative top-k", []string{"sync-errors", "cn1", "--app-id", "a1", "--top-k", "-1"}},
		{"compare no right", []string{"sync-compare", "cn1", "--app-id", "a1", "--left", "L"}},
		{"compare no left", []string{"sync-compare", "cn1", "--app-id", "a1", "--right", "R"}},
		{"compare body-file with left", []string{"sync-compare", "cn1", "--app-id", "a1", "--left", "L", "--body-file", file}},
		{"compare null body-file", []string{"sync-compare", "cn1", "--app-id", "a1", "--body-file", file}},
		{"changes blank since", []string{"sync-changes", "cn1", "--app-id", "a1", "--since", " "}},
		{"history dotdot id", []string{"sync-history", "..", "--app-id", "a1"}},
		{"history no app", []string{"sync-history", "cn1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			srv, seen := syncDiagServer(t, fixedReply(200, `{}`))
			r := runMutationCLI(t, srv, 0, nil, append([]string{"connectors"}, tc.args...)...)
			if r.exit != exitUsage {
				t.Errorf("exit %d (%v), want %d", r.exit, r.err, exitUsage)
			}
			if got := seen(); len(got) != 0 {
				t.Errorf("sent %+v, want nothing", got)
			}
		})
	}
}

func TestSyncDiagnosticsServerStatusExits(t *testing.T) {
	for _, tc := range []struct {
		status, want int
	}{{404, exitNotFound}, {400, exitUsage}, {500, exitServer}} {
		t.Setenv("C1I_URL", mutTestURL)
		srv, _ := syncDiagServer(t, fixedReply(tc.status, `{"code":5,"message":"not found"}`))
		for _, args := range [][]string{
			{"sync-history", "cn1", "--app-id", "a1"},
			{"sync-errors", "cn1", "--app-id", "a1"},
			{"sync-changes", "cn1", "--app-id", "a1"},
			{"sync-compare", "cn1", "--app-id", "a1", "--left", "L", "--right", "R"},
		} {
			r := runMutationCLI(t, srv, 0, nil, append([]string{"connectors"}, args...)...)
			if r.exit != tc.want {
				t.Errorf("%s with %d: exit %d, want %d", args[0], tc.status, r.exit, tc.want)
			}
		}
	}
}

func TestConnectorsDebuggablePaginatesThroughEmptyPage(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	srv, seen := syncDiagServer(t, func(r *http.Request, n int) mutReply {
		switch r.URL.Query().Get("page_token") {
		case "":
			return mutReply{body: `{"connectors":[{"appId":"a1","connectorId":"c1","authored":false}],"nextPageToken":"t1"}`}
		case "t1":
			return mutReply{body: `{"connectors":[],"nextPageToken":"t2"}`}
		default:
			return mutReply{body: `{"connectors":[{"appId":"a2","connectorId":"c2","authored":true}]}`}
		}
	})
	r := runCLI(t, func() {
		stubMutationClient(t, srv, 0)
		orig := newListClient
		newListClient = newClient
		t.Cleanup(func() { newListClient = orig })
	}, nil, "connectors", "debuggable", "--page-size", "500")
	if r.exit != 0 {
		t.Fatalf("exit %d (%v)", r.exit, r.err)
	}
	lines := strings.Split(strings.TrimSpace(r.stdout), "\n")
	if len(lines) != 2 || !strings.Contains(lines[1], `"connectorId":"c2"`) || !strings.Contains(lines[1], `"authored":true`) {
		t.Fatalf("stdout %q, want 2 NDJSON rows with real bool types", r.stdout)
	}
	got := seen()
	if len(got) != 3 || got[0].path != "/api/v1/sync_diagnostics/connectors" || !strings.Contains(got[0].query, "page_size=50") {
		t.Errorf("requests %+v, want 3 pages, first page_size=50", got)
	}
}

func TestSyncDiagnosticsPostRetriedOn5xx(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	srv, seen := syncDiagServer(t, func(_ *http.Request, n int) mutReply {
		if n == 1 {
			return mutReply{status: 503, body: `{}`}
		}
		return mutReply{body: `{"events":[]}`}
	})
	r := runMutationCLI(t, srv, 2, nil, "connectors", "sync-changes", "cn1", "--app-id", "a1")
	if r.exit != 0 || len(seen()) != 2 {
		t.Fatalf("exit %d, %d requests; want a retried read to exit 0 after 2", r.exit, len(seen()))
	}
}
