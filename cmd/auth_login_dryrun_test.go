package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// auth login can't preview a login, so --dry-run must stop it before any
// request (device code or credential check) is sent.
func TestAuthLoginRejectsDryRun(t *testing.T) {
	cases := []struct {
		name string
		args []string
		env  bool
	}{
		{"browser, flag", []string{"--dry-run"}, false},
		{"browser, env", nil, true},
		{"client credentials, flag", []string{"--client-id", "id", "--client-secret", "secret", "--dry-run"}, false},
		{"client credentials, env", []string{"--client-id", "id", "--client-secret", "secret"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			t.Cleanup(srv.Close)
			resetCmds(t, authLoginCmd)
			resetRootDryRunFlag(t)
			t.Setenv("C1I_DRY_RUN", "")
			if tc.env {
				t.Setenv("C1I_DRY_RUN", "1")
			}
			origTTY := isTerminal
			isTerminal = func() bool { return false }
			t.Cleanup(func() { isTerminal = origTTY })
			t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil) })

			err := runRootWithArgs(t, append([]string{"auth", "login", "--url", srv.URL}, tc.args...))
			if got := exitCode(err); got != exitUsage {
				t.Fatalf("exitCode = %d, want %d; err: %v", got, exitUsage, err)
			}
			if !strings.Contains(err.Error(), "--dry-run is unsupported for auth login") {
				t.Errorf("error = %q, want the dry-run explanation", err)
			}
			if n := requests.Load(); n != 0 {
				t.Errorf("%d requests sent; want none", n)
			}
		})
	}
}
