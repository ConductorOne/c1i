package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/viper"
)

// TestAPIGuardsExitUsage is a table test covering every bad flag/argument
// combination in `c1i api` that must classify as exitUsage (2), not the
// generic exitError (1) they returned before each guard was wrapped in
// &usageError{}. This repo documents 2 as "bad flags or arguments" and tells
// agents to branch on exit codes rather than parse stderr text, so a usage
// mistake landing on the generic code silently breaks that contract.
//
// Each case asserts via exitCode(err), never by string-matching the error
// text — the guard's message text is intentionally unchanged by this fix, so
// asserting on exit code (the thing the fix actually touches) is what proves
// the fix rather than the message.
func TestAPIGuardsExitUsage(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{
			name: "limit without paginate",
			args: []string{"api", "--path", "/api/v1/apps", "--limit", "5"},
		},
		{
			name: "list-key without paginate",
			args: []string{"api", "--path", "/api/v1/apps", "--list-key", "list"},
		},
		{
			name: "body and body-file mutually exclusive",
			args: []string{"api", "--path", "/api/v1/apps", "--body", `{}`, "--body-file", "/nonexistent"},
		},
		{
			name: "unsupported method",
			args: []string{"api", "--path", "/api/v1/apps", "--method", "TRACE"},
		},
		{
			name: "GET does not take a body",
			args: []string{"api", "--path", "/api/v1/apps", "--method", "GET", "--body", `{}`},
		},
		{
			name: "DELETE does not take a body without the opt-in",
			args: []string{"api", "--path", "/api/v1/apps", "--method", "DELETE", "--body", `{}`},
		},
		{
			name: "invalid --query key=value",
			args: []string{"api", "--path", "/api/v1/apps", "--query", "no-equals-sign"},
		},
		{
			name: "invalid --header key=value",
			args: []string{"api", "--path", "/api/v1/apps", "--header", "no-equals-sign"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			resetAPICmdFlags(t)
			t.Setenv("C1I_URL", "https://example.invalid")

			var out bytes.Buffer
			apiCmd.SetOut(&out)
			apiCmd.SetErr(&out)
			rootCmd.SetArgs(tc.args)

			err := rootCmd.ExecuteContext(context.Background())
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if got, want := exitCode(err), exitUsage; got != want {
				t.Errorf("exitCode(%v) = %d, want %d (exitUsage)", err, got, want)
			}
		})
	}
}

// TestAPIInvalidJSONBodyExitsUsage covers the three "invalid JSON body"
// fmt.Errorf sites in cmd/api.go (the --dry-run preview path, the live
// POST/PUT/PATCH path, and the --allow-delete-body DELETE path) found after
// the rest of this sweep landed: a malformed --body/--body-file is exactly
// the caller-fixable class TestAPIGuardsExitUsage already covers, so these
// three must classify as exitUsage too, not exitError.
func TestAPIInvalidJSONBodyExitsUsage(t *testing.T) {
	t.Run("dry-run preview", func(t *testing.T) {
		resetAPICmdFlags(t)
		t.Setenv("C1I_URL", "https://example.invalid")

		origDryRun := viper.GetBool("dry_run")
		viper.Set("dry_run", true)
		t.Cleanup(func() { viper.Set("dry_run", origDryRun) })

		var out bytes.Buffer
		apiCmd.SetOut(&out)
		apiCmd.SetErr(&out)
		rootCmd.SetArgs([]string{"api", "--path", "/api/v1/apps", "--method", "POST", "--body", "not json"})

		err := rootCmd.ExecuteContext(context.Background())
		if err == nil {
			t.Fatal("expected an error, got nil")
		}
		if got, want := exitCode(err), exitUsage; got != want {
			t.Errorf("exitCode(%v) = %d, want %d (exitUsage)", err, got, want)
		}
	})

	t.Run("live POST", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("unexpected request: %s %s -- malformed body should never reach the wire", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		resetAPICmdFlags(t)
		t.Setenv("C1I_URL", "https://example.invalid")
		stubNewAPIClient(t, srv)

		var out bytes.Buffer
		apiCmd.SetOut(&out)
		apiCmd.SetErr(&out)
		rootCmd.SetArgs([]string{"api", "--path", "/api/v1/apps", "--method", "POST", "--body", "not json"})

		err := rootCmd.ExecuteContext(context.Background())
		if err == nil {
			t.Fatal("expected an error, got nil")
		}
		if got, want := exitCode(err), exitUsage; got != want {
			t.Errorf("exitCode(%v) = %d, want %d (exitUsage)", err, got, want)
		}
	})

	t.Run("live DELETE with --allow-delete-body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("unexpected request: %s %s -- malformed body should never reach the wire", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		resetAPICmdFlags(t)
		t.Setenv("C1I_URL", "https://example.invalid")
		stubNewAPIClient(t, srv)

		var out bytes.Buffer
		apiCmd.SetOut(&out)
		apiCmd.SetErr(&out)
		rootCmd.SetArgs([]string{"api", "--path", "/api/v1/apps", "--method", "DELETE", "--allow-delete-body", "--body", "not json"})

		err := rootCmd.ExecuteContext(context.Background())
		if err == nil {
			t.Fatal("expected an error, got nil")
		}
		if got, want := exitCode(err), exitUsage; got != want {
			t.Errorf("exitCode(%v) = %d, want %d (exitUsage)", err, got, want)
		}
	})
}
