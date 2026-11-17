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

// With --page-token, functions usage scans one page; an empty result must say
// so on stderr (never stdout, which carries rows only).
func TestFunctionsUsagePageTokenNoMatchNotice(t *testing.T) {
	cases := []struct {
		name  string
		token string
		want  string
	}{
		{"with page token", "tok1", "no automations on this page call function func1 (--page-token scans one page only)"},
		{"without page token", "", "no automations call function func1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `{"list":[{"id":"a1"}]}`)
			}))
			defer srv.Close()
			orig := newListClient
			newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
				return client.NewForTesting(srv.URL, srv.Client()), nil
			}
			t.Cleanup(func() { newListClient = orig })
			t.Setenv("C1I_URL", "https://example.invalid")

			resetCmdFlags(t, functionsUsageCmd)
			if tc.token != "" {
				if err := functionsUsageCmd.Flags().Set("page-token", tc.token); err != nil {
					t.Fatal(err)
				}
			}
			var out, errOut strings.Builder
			functionsUsageCmd.SetOut(&out)
			functionsUsageCmd.SetErr(&errOut)
			functionsUsageCmd.SetContext(context.Background())
			if err := functionsUsageCmd.RunE(functionsUsageCmd, []string{"func1"}); err != nil {
				t.Fatalf("RunE: %v", err)
			}
			if out.Len() != 0 {
				t.Errorf("stdout = %q, want empty", out.String())
			}
			if !strings.Contains(errOut.String(), tc.want) {
				t.Errorf("stderr = %q, want it to contain %q", errOut.String(), tc.want)
			}
		})
	}
}
