package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

const msBindingsPath = "/api/v1/apps/app-1/resource_types/rt-1/managed_state_bindings"

var msScope = []string{"--app-id", "app-1", "--resource-type-id", "rt-1"}

func TestManagedStateRow(t *testing.T) {
	var managed, unmanaged, neither managedStateBinding
	if err := json.Unmarshal([]byte(`{"resourceId":"r1","displayName":"Demo","createdAt":"c","updatedAt":"u","state":{"managed":{"appId":"app-9"}}}`), &managed); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"resourceId":"r2","state":{"unmanaged":{}}}`), &unmanaged); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"resourceId":"r3","state":null}`), &neither); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		in   managedStateBinding
		want map[string]any
	}{
		{"managed", managed, map[string]any{"resource_id": "r1", "display_name": "Demo", "state": "managed", "managed_app_id": "app-9", "created_at": "c", "updated_at": "u"}},
		{"unmanaged: managed_app_id is nil, not empty", unmanaged, map[string]any{"resource_id": "r2", "display_name": nil, "state": "unmanaged", "managed_app_id": nil, "created_at": nil, "updated_at": nil}},
		{"no state: state is nil, not a guess", neither, map[string]any{"resource_id": "r3", "display_name": nil, "state": nil, "managed_app_id": nil, "created_at": nil, "updated_at": nil}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := managedStateRow(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("row = %v, want %v", got, tc.want)
			}
		})
	}
}

// A null wrapper is skipped, not printed as an all-null row.
func TestManagedStateListSkipsNullWrapper(t *testing.T) {
	var tc listPaginationCase
	for _, c := range listPaginationCases() {
		if c.name == "apps managed-state list" {
			tc = c
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"list":[{"appManagementStateBinding":null},{"appManagementStateBinding":{"resourceId":"r1","state":{"unmanaged":{}}}}],"nextPageToken":""}`))
	}))
	defer srv.Close()
	out, errOut, err := runListPaginationCase(t, tc, srv)
	if err != nil {
		t.Fatalf("RunE: %v (stderr %q)", err, errOut)
	}
	rows := decodeNDJSONRows(t, out)
	if len(rows) != 1 || rows[0]["resource_id"] != "r1" || rows[0]["state"] != "unmanaged" || rows[0]["managed_app_id"] != nil {
		t.Errorf("rows = %v, want the single r1 row", rows)
	}
}

func TestManagedStatePromoteBody(t *testing.T) {
	cases := []struct {
		name  string
		extra []string
		want  string
		reads int
	}{
		{"no owners sends {} so the server inherits", nil, `{}`, 1},
		{"owners are sent once each", []string{"--user-id", "u1", "--user-id", "u2", "--user-id", "u1"}, `{"userIds":["u1","u2"]}`, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, fixedReply(200, tblBinding))
			args := append(append([]string{"apps", "managed-state", "promote", "res-1"}, msScope...), tc.extra...)
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.err != nil {
				t.Fatalf("exit %d: %v\n%s", r.exit, r.err, r.stderr)
			}
			sent := s.requests()
			if len(sent) != tc.reads+1 {
				t.Fatalf("requests = %+v, want %d reads and one write", sent, tc.reads)
			}
			w := sent[len(sent)-1]
			if w.method != http.MethodPost || w.rawPath != msBindingsPath+"/res-1/promote" || w.body != tc.want {
				t.Errorf("write = %s %s %s, want POST %s/res-1/promote %s", w.method, w.rawPath, w.body, msBindingsPath, tc.want)
			}
		})
	}
}

// An unknown binding exits 4 before any POST, a --dry-run included; an id with
// a slash stays one path segment.
func TestManagedStatePromoteUnknownBindingExits4(t *testing.T) {
	for _, dry := range []bool{false, true} {
		name := "real"
		if dry {
			name = "dry-run"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			viper.Set("dry_run", dry)
			t.Cleanup(func() { viper.Set("dry_run", nil) })
			s := newMutServer(t, fixedReply(404, `{"code":5,"message":"not found"}`))
			args := append([]string{"apps", "managed-state", "promote", "a/b"}, msScope...)
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.exit != exitNotFound || r.stdout != "" {
				t.Errorf("exit %d stdout %q, want exit 4 and nothing printed", r.exit, r.stdout)
			}
			sent := s.requests()
			if len(sent) != 1 || sent[0].method != http.MethodGet || sent[0].rawPath != msBindingsPath+"/a%2Fb" {
				t.Errorf("requests = %+v, want one GET of the escaped binding path", sent)
			}
			if !strings.Contains(r.stderr, "no such discovered app") {
				t.Errorf("stderr = %q, want the no-such-discovered-app line", r.stderr)
			}
		})
	}
}

func TestManagedStateGetUnknownExits4(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, fixedReply(404, `{"code":5,"message":"not found"}`))
	r := runMutationCLI(t, s.srv, 0, nil, append([]string{"apps", "managed-state", "get", "res-1"}, msScope...)...)
	if r.exit != exitNotFound || r.stdout != "" {
		t.Errorf("exit %d stdout %q, want exit 4 and nothing printed", r.exit, r.stdout)
	}
}
