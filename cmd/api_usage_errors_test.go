package cmd

import (
	"bytes"
	"context"
	"testing"
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
