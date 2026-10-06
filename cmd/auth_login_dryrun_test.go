package cmd

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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
			// Count connections at accept, so a request counts even when its
			// TLS handshake later fails.
			var conns atomic.Int32
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
				if s == http.StateNew {
					conns.Add(1)
				}
			}
			srv.StartTLS()
			t.Cleanup(srv.Close)

			// A credential check that reaches the server succeeds, so only the
			// connection count can catch a guard that runs after it.
			origCred := newCredentialClient
			newCredentialClient = func(*cobra.Command, string, string, string) (*client.Client, error) {
				return client.NewForTesting(srv.URL, srv.Client(), client.WithMaxRetries(0)), nil
			}
			t.Cleanup(func() { newCredentialClient = origCred })

			resetCmds(t, authLoginCmd)
			resetRootDryRunFlag(t)
			// Other tests leave a viper.Set override, which beats the flag and
			// env; a nil override falls through to them.
			viper.Set("dry_run", nil)
			t.Cleanup(func() { viper.Set("dry_run", nil) })
			t.Setenv("C1I_DRY_RUN", "")
			if tc.env {
				t.Setenv("C1I_DRY_RUN", "1")
			}
			origTTY := isTerminal
			isTerminal = func() bool { return false }
			t.Cleanup(func() { isTerminal = origTTY })
			t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil) })

			err := runRootWithArgs(t, append([]string{"auth", "login", "--url", srv.URL}, tc.args...))
			if n := conns.Load(); n != 0 {
				t.Errorf("%d connections opened; want none", n)
			}
			if got := exitCode(err); got != exitUsage {
				t.Fatalf("exitCode = %d, want %d; err: %v", got, exitUsage, err)
			}
			if !strings.Contains(err.Error(), "--dry-run is unsupported for auth login") {
				t.Errorf("error = %q, want the dry-run explanation", err)
			}
		})
	}
}
