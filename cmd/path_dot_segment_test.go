package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// A "." or ".." id is normalized server-side to the parent or collection, and
// a non-GET may be routed there with no redirect for the redirect guard to
// see, so the request must be refused before it is sent.
func TestDotSegmentIDIsUsageErrorAndSendsNothing(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"users get dot", []string{"users", "get", "."}},
		{"users get dot-dot", []string{"users", "get", ".."}},
		{"apps get dot-dot", []string{"apps", "get", ".."}},
		{"users get dot-dot matrix", []string{"users", "get", "..;"}},
		{"api dot-dot matrix", []string{"api", "--path", "/api/v1/users/x/..;/abc"}},
		{"api encoded dot", []string{"api", "--path", "/api/v1/users/%2E"}},
		{"api interior dot POST", []string{"api", "--method", "POST", "--path", "/api/v1/webhooks/./test", "--body", "{}"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("request sent: %s %s", r.Method, r.URL.EscapedPath())
			}))
			t.Cleanup(srv.Close)
			orig := newClient
			newClient = func(_ *cobra.Command, baseURL string) (*client.Client, error) {
				return client.NewForTesting(baseURL, srv.Client(), client.WithMaxRetries(0)), nil
			}
			t.Cleanup(func() { newClient = orig })
			resetCmdFlags(t, rootCmd)
			resetCmdFlags(t, apiCmd)
			t.Setenv("C1I_URL", srv.URL)

			err := runRootWithArgs(t, tc.args)
			if got, want := exitCode(err), exitUsage; got != want {
				t.Fatalf("exitCode = %d, want %d (err: %v)", got, want, err)
			}
			if !strings.Contains(displayError(err).Error(), "refusing to send") {
				t.Errorf("error = %q, want the PathError refusal", displayError(err))
			}
		})
	}
}
