package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// TestAppRowUserCountIsNumeric pins that user_count keeps its real numeric
// type. The API sends it as a JSON string ("103"); emitting that string
// unchanged makes every string sort above every number in jq, so
// `jq 'select(.user_count > 60)'` would match every row regardless of value.
func TestAppRowUserCountIsNumeric(t *testing.T) {
	var item appListItem
	if err := json.Unmarshal([]byte(`{"id":"a1","userCount":"103"}`), &item); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	row := appRow(item)

	v, ok := row["user_count"].(int64)
	if !ok {
		t.Fatalf("user_count has type %T, want int64", row["user_count"])
	}
	if v != 103 {
		t.Errorf("user_count = %d, want 103", v)
	}
}

// `apps list` omits deleted apps, so its rows carry no deleted_at (it was
// always null); apps get shows deletedAt.
func TestAppsListRowsHaveNoDeletedAt(t *testing.T) {
	if _, ok := appRow(appListItem{ID: "a1"})["deleted_at"]; ok {
		t.Error("row still has a deleted_at key")
	}
	t.Setenv("C1I_URL", mutTestURL)
	clearViperOverrides(t, "fields") // another test may have left an override
	s := newMutServer(t, fixedReply(200, `{"list":[{"id":"a1","displayName":"A","userCount":"3"}]}`))
	stub := func() {
		orig := newListClient
		newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
			return client.NewForTesting(s.srv.URL, s.srv.Client()), nil
		}
		t.Cleanup(func() { newListClient = orig })
	}
	r := runCLI(t, stub, nil, "apps", "list")
	if r.err != nil || strings.Contains(r.stdout, "deleted_at") {
		t.Errorf("exit %d (%v), stdout %q", r.exit, r.err, r.stdout)
	}
	r = runCLI(t, stub, nil, "apps", "list", "--fields", "deleted_at")
	if r.exit != exitUsage {
		t.Errorf("--fields deleted_at: exit %d (%v), want %d (matched no keys)", r.exit, r.err, exitUsage)
	}
}

// TestAppsSearchWire pins the request body each filter flag produces, that
// rows match `apps list`, and that no filter set sends no filter keys.
func TestAppsSearchWire(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	clearViperOverrides(t, "fields")
	reply := `{"list":[{"id":"a1","displayName":"A","userCount":"3"}]}`
	run := func(args ...string) (*mutServer, mutResult) {
		s := newMutServer(t, func(r *http.Request, _ int) mutReply {
			if strings.HasPrefix(r.URL.Path, "/api/v1/policies/") {
				return mutReply{status: 200, body: `{"policy":{"policyType":"POLICY_TYPE_GRANT"}}`}
			}
			return mutReply{status: 200, body: reply}
		})
		stub := func() {
			orig := newListClient
			newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
				return client.NewForTesting(s.srv.URL, s.srv.Client()), nil
			}
			t.Cleanup(func() { newListClient = orig })
		}
		return s, runCLI(t, stub, nil, append([]string{"apps", "search"}, args...)...)
	}
	s, r := run("--query", "q", "--display-name", "D", "--app-id", "i1", "--app-id", "i2",
		"--exclude-app-id", "x1", "--policy-id", "p1", "--policy-id", "p1", "--policy-id", "p2", "--only-directories", "--limit", "1")
	if r.err != nil {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
	if len(s.seen) != 3 || s.seen[2].method != "POST" || s.seen[2].rawPath != "/api/v1/search/apps" {
		t.Fatalf("requests (2 policy reads, then the search): %+v", s.seen)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(s.seen[2].body), &got); err != nil {
		t.Fatal(err)
	}
	want := `{"appIds":["i1","i2"],"displayName":"D","excludeAppIds":["x1"],"onlyDirectories":true,` +
		`"pageSize":1,"policyRefs":[{"id":"p1"},{"id":"p2"}],"query":"q"}`
	gb, _ := json.Marshal(got)
	if string(gb) != want {
		t.Errorf("body\n got %s\nwant %s", gb, want)
	}
	if !strings.Contains(r.stdout, `"user_count":3`) || !strings.Contains(r.stdout, `"display_name":"A"`) {
		t.Errorf("row shape: %q", r.stdout)
	}
	s, r = run("--limit", "1")
	if r.err != nil || strings.Contains(s.seen[0].body, "query") || strings.Contains(s.seen[0].body, "Ids") {
		t.Errorf("bare search sent filters: %v %q", r.err, s.seen[0].body)
	}
}

// An unknown or deleted --policy-id is refused before any search is sent: the
// server ignores an unknown policy ref and would return every app.
func TestAppsSearchPolicyIDIsChecked(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	clearViperOverrides(t, "fields")
	cases := []struct {
		name   string
		status int
		body   string
		exit   int
		search bool
	}{
		{"unknown", 404, `{"code":5,"message":"not found"}`, exitNotFound, false},
		{"deleted", 200, `{"policy":{"policyType":"POLICY_TYPE_GRANT","deletedAt":"2024-01-01T00:00:00Z"}}`, exitUsage, false},
		{"exists", 200, `{"policy":{"policyType":"POLICY_TYPE_GRANT"}}`, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newMutServer(t, func(r *http.Request, _ int) mutReply {
				if strings.HasPrefix(r.URL.Path, "/api/v1/policies/") {
					return mutReply{status: tc.status, body: tc.body}
				}
				return mutReply{status: 200, body: `{"list":[]}`}
			})
			stub := func() {
				orig := newListClient
				newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
					return client.NewForTesting(s.srv.URL, s.srv.Client()), nil
				}
				t.Cleanup(func() { newListClient = orig })
			}
			r := runCLI(t, stub, nil, "apps", "search", "--policy-id", "p1", "--policy-id", "p1")
			if r.exit != tc.exit {
				t.Errorf("exit %d (%v), want %d", r.exit, r.err, tc.exit)
			}
			var searched, gets int
			for _, q := range s.seen {
				if q.rawPath == "/api/v1/search/apps" {
					searched++
				} else {
					gets++
				}
			}
			if (searched > 0) != tc.search || gets != 1 {
				t.Errorf("searches %d (want sent=%v), policy reads %d (want 1, deduped)", searched, tc.search, gets)
			}
			if tc.exit != 0 && !strings.Contains(r.stderr+fmt.Sprint(r.err), `--policy-id "p1"`) {
				t.Errorf("message does not name flag and id: %q %v", r.stderr, r.err)
			}
		})
	}
}
