package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// runTestConnection runs `mcp servers test-connection` against a stub that
// answers status/body, and returns stdout, the error, and the --error-format
// json rendering of that error.
func runTestConnection(t *testing.T, status int, body string) (stdout, errJSON string, err error) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/mcp_servers/test_connection" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	orig := newClient
	newClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newClient = orig })
	t.Setenv("C1I_URL", "https://example.invalid")

	resetCmds(t, rootCmd, mcpServersTestConnectionCmd)
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil) })
	mcpServersTestConnectionCmd.SetContext(t.Context())
	rootCmd.SetArgs([]string{"mcp", "servers", "test-connection", "--server-url", "https://mcp.example.invalid"})
	err = rootCmd.ExecuteContext(t.Context())
	if err != nil {
		var eb bytes.Buffer
		writeError(&eb, err, "json", mcpServersTestConnectionCmd)
		errJSON = eb.String()
	}
	return out.String(), errJSON, err
}

func TestTestConnectionUnreachableExitsUpstream(t *testing.T) {
	const body = `{"reachable":false,"toolCount":"0","failureReason":"connection refused"}`
	for _, tc := range []struct{ name, body string }{
		{"explicit false", body},
		{"reachable omitted", `{"toolCount":"0","failureReason":"connection refused"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, errJSON, err := runTestConnection(t, 200, tc.body)
			if got := exitCode(err); got != exitUpstream {
				t.Fatalf("exit = %d (%v), want %d", got, err, exitUpstream)
			}
			if !strings.Contains(err.Error(), "connection refused") {
				t.Errorf("error %q does not name failureReason", err)
			}
			var in, out map[string]any
			_ = json.Unmarshal([]byte(tc.body), &in)
			if jerr := json.Unmarshal([]byte(stdout), &out); jerr != nil || len(out) != len(in) || out["failureReason"] != in["failureReason"] {
				t.Errorf("stdout = %q, want the response JSON", stdout)
			}
			var obj map[string]any
			if jerr := json.Unmarshal([]byte(errJSON), &obj); jerr != nil || obj["error"] != err.Error() {
				t.Errorf("--error-format json = %q, want {\"error\": %q}", errJSON, err)
			}
		})
	}
}

func TestTestConnectionUnrecognizedShapeExitsServer(t *testing.T) {
	for _, body := range []string{`null`, `[]`, `"x"`, `3`} {
		stdout, _, err := runTestConnection(t, 200, body)
		if got := exitCode(err); got != exitServer {
			t.Errorf("body %s: exit = %d (%v), want %d", body, got, err, exitServer)
		}
		if !strings.Contains(stdout, strings.TrimSpace(body)) {
			t.Errorf("body %s: stdout = %q, want it still printed", body, stdout)
		}
	}
}

func TestTestConnectionReachableExitsZero(t *testing.T) {
	stdout, _, err := runTestConnection(t, 200, `{"reachable":true,"toolCount":"3"}`)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !strings.Contains(stdout, `"reachable": true`) {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestTestConnectionAPIErrorKeepsClassification(t *testing.T) {
	for status, want := range map[int]int{404: exitNotFound, 500: exitServer, 403: exitAuth} {
		_, _, err := runTestConnection(t, status, `{"message":"x"}`)
		if got := exitCode(err); got != want {
			t.Errorf("status %d: exit = %d (%v), want %d", status, got, err, want)
		}
	}
}
