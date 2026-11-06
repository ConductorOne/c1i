package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// runUsersList runs users list against a stub that records each request body
// and serves two pages of one user each, carrying origin when it is non-empty.
func runUsersList(t *testing.T, origin string, origins ...string) ([]map[string]any, string, error) {
	t.Helper()
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var b map[string]any
		_ = json.Unmarshal(raw, &b)
		bodies = append(bodies, b)
		id := fmt.Sprintf("u%d", len(bodies))
		user := fmt.Sprintf(`{"id":%q,"status":"ENABLED"}`, id)
		if origin != "" {
			user = fmt.Sprintf(`{"id":%q,"status":"ENABLED","origin":%q}`, id, origin)
		}
		next := ""
		if len(bodies) == 1 {
			next = "p2"
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"list":[{"user":%s}],"nextPageToken":%q}`, user, next)
	}))
	t.Cleanup(srv.Close)

	orig := newListClient
	newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newListClient = orig })
	t.Setenv("C1I_URL", "https://example.invalid")

	resetCmdFlags(t, usersListCmd)
	for _, o := range origins {
		if err := usersListCmd.Flags().Set("origin", o); err != nil {
			t.Fatalf("setting --origin %q: %v", o, err)
		}
	}
	var out strings.Builder
	usersListCmd.SetOut(&out)
	usersListCmd.SetErr(io.Discard)
	usersListCmd.SetContext(context.Background())
	err := usersListCmd.RunE(usersListCmd, nil)
	return bodies, out.String(), err
}

func TestUsersListOriginSendsOriginsFilter(t *testing.T) {
	all := []any{"USER_ORIGIN_DIRECTORY", "USER_ORIGIN_LOCAL", "USER_ORIGIN_SYSTEM"}
	for _, tc := range []struct {
		name    string
		origins []string
		want    []any // nil: no origins key sent
	}{
		{"unset sends no filter", nil, nil},
		{"one", []string{"local"}, []any{"USER_ORIGIN_LOCAL"}},
		{"case-insensitive, repeatable", []string{"Directory", "SYSTEM"}, []any{"USER_ORIGIN_DIRECTORY", "USER_ORIGIN_SYSTEM"}},
		{"all", []string{"all"}, all},
		{"all with a duplicate", []string{"local", "all"}, all},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bodies, _, err := runUsersList(t, "", tc.origins...)
			if err != nil {
				t.Fatalf("RunE: %v", err)
			}
			if len(bodies) != 2 {
				t.Fatalf("requests = %d, want 2", len(bodies))
			}
			for i, b := range bodies { // every page, not just the first
				got, ok := b["origins"]
				if tc.want == nil {
					if ok {
						t.Fatalf("page %d: origins = %v, want key absent", i+1, got)
					}
					continue
				}
				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("page %d: origins = %v, want %v", i+1, got, tc.want)
				}
			}
		})
	}
}

func TestUsersListUnknownOriginIsUsageError(t *testing.T) {
	bodies, _, err := runUsersList(t, "", " Service ")
	if code := exitCode(err); code != exitUsage {
		t.Fatalf("exit = %d (err %v), want %d", code, err, exitUsage)
	}
	if len(bodies) != 0 {
		t.Fatalf("sent %d requests, want none", len(bodies))
	}
	if !strings.Contains(err.Error(), `" Service "`) || !strings.Contains(err.Error(), "directory, local, system, all") {
		t.Fatalf("error %q should quote the input and list the accepted values", err)
	}
}

func TestUsersListRowCarriesOrigin(t *testing.T) {
	_, out, err := runUsersList(t, "USER_ORIGIN_LOCAL", "all")
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	rows := decodeNDJSONRows(t, out)
	if len(rows) != 2 || rows[0]["origin"] != "USER_ORIGIN_LOCAL" {
		t.Fatalf("rows = %v, want origin USER_ORIGIN_LOCAL", rows)
	}

	// Absent from the response: omit the key rather than emit "".
	_, out, err = runUsersList(t, "")
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	rows = decodeNDJSONRows(t, out)
	if _, ok := rows[0]["origin"]; ok {
		t.Fatalf("row = %v, want no origin key", rows[0])
	}
}
