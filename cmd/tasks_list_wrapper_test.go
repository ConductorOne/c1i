package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// A search/tasks item without its task wrapper must be skipped, not emitted
// as a row of zero values.
func TestTaskListsSkipMissingTaskWrapper(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cmd   *cobra.Command
		flags map[string]string
	}{
		{"tasks list", tasksListCmd, nil},
		// --all skips the introspect lookup for the default "me" scope.
		{"requests list", requestsListCmd, map[string]string{"all": "true"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `{"list":[{"task":{"id":"t1"}},{"task":null},{}],"nextPageToken":""}`)
			}))
			defer srv.Close()

			orig := newListClient
			newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
				return client.NewForTesting(srv.URL, srv.Client()), nil
			}
			t.Cleanup(func() { newListClient = orig })
			t.Setenv("C1I_URL", "https://example.invalid")

			resetCmdFlags(t, tc.cmd)
			for k, v := range tc.flags {
				if err := tc.cmd.Flags().Set(k, v); err != nil {
					t.Fatalf("setting --%s: %v", k, err)
				}
			}
			var out strings.Builder
			tc.cmd.SetOut(&out)
			tc.cmd.SetContext(context.Background())
			if err := tc.cmd.RunE(tc.cmd, nil); err != nil {
				t.Fatalf("RunE: %v", err)
			}
			rows := decodeNDJSONRows(t, out.String())
			if len(rows) != 1 || rows[0]["id"] != "t1" {
				t.Fatalf("rows = %v, want only t1", rows)
			}
		})
	}
}
