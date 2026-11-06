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

// runFunctionsUsage serves one automations page and runs
// `functions usage func1` with the given flags.
func runFunctionsUsage(t *testing.T, listJSON string, flags map[string]string) (stdout, stderr string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"list":%s,"nextPageToken":""}`, listJSON)
	}))
	defer srv.Close()

	orig := newListClient
	newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newListClient = orig })
	t.Setenv("C1I_URL", "https://example.invalid")

	resetCmdFlags(t, functionsUsageCmd)
	for k, v := range flags {
		if err := functionsUsageCmd.Flags().Set(k, v); err != nil {
			t.Fatalf("setting --%s: %v", k, err)
		}
	}
	var out, errOut strings.Builder
	functionsUsageCmd.SetOut(&out)
	functionsUsageCmd.SetErr(&errOut)
	functionsUsageCmd.SetContext(context.Background())
	if err := functionsUsageCmd.RunE(functionsUsageCmd, []string{"func1"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	return out.String(), errOut.String()
}

// With --page-token only one page is scanned, so "no automations call
// function X" would overclaim.
func TestFunctionsUsageNoMatchMessageScopedToPageUnderPageToken(t *testing.T) {
	_, stderr := runFunctionsUsage(t, `[{"id":"a1"}]`, map[string]string{"page-token": "tok1"})
	if !strings.Contains(stderr, "no automations on this page call function func1") {
		t.Errorf("stderr = %q, want the one-page message", stderr)
	}

	_, stderr = runFunctionsUsage(t, `[{"id":"a1"}]`, nil)
	if !strings.Contains(stderr, "no automations call function func1") {
		t.Errorf("stderr = %q, want the whole-set message when auto-paginating", stderr)
	}
}

func TestFunctionsUsageNeverExecutedIsNullNotEmptyString(t *testing.T) {
	stdout, _ := runFunctionsUsage(t,
		`[{"id":"a1","automationSteps":[{"stepName":"s1","callFunction":{"functionId":"func1"}}]}]`, nil)
	rows := decodeNDJSONRows(t, stdout)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	v, ok := rows[0]["last_executed_at"]
	if !ok || v != nil {
		t.Errorf("last_executed_at = %#v (present=%v), want JSON null", v, ok)
	}
}
