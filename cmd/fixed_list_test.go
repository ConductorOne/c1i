package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// fixedListWarning is the stable substring of getFixedList's partial-result
// warning.
const fixedListWarning = "returned a partial result"

func fixedListCommands(t *testing.T) map[string]*cobra.Command {
	t.Helper()
	sub := func(group *cobra.Command, name string) *cobra.Command {
		for _, c := range group.Commands() {
			if c.Name() == name {
				return c
			}
		}
		t.Fatalf("%s has no %q subcommand", group.CommandPath(), name)
		return nil
	}
	return map[string]*cobra.Command{
		"role-mining suggestions list":       roleMiningSuggestionsListCmd,
		"role-mining runs list":              roleMiningRunsListCmd,
		"role-mining custom-analysis list":   roleMiningCustomAnalysisListCmd,
		"findings routing-rules list":        sub(findingsRoutingRulesCmd, "list"),
		"findings transformation-rules list": sub(findingsTransformationRulesCmd, "list"),
	}
}

func runFixedList(t *testing.T, cmd *cobra.Command, body string, flags map[string]string) (stdout, stderr string) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	stubNewClient(t, srv)
	t.Setenv("C1I_URL", "https://example.invalid")
	resetCmdFlags(t, cmd)
	for name, val := range flags {
		if err := cmd.Flags().Set(name, val); err != nil {
			t.Fatalf("setting --%s: %v", name, err)
		}
	}
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	t.Cleanup(func() { cmd.SetOut(nil); cmd.SetErr(nil) })
	cmd.SetContext(context.Background())
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	return out.String(), errOut.String()
}

// These endpoints document no page_size/page_token, so the commands fetch one
// response. If the server ever returns a cursor anyway, the rows are a
// truncated set and must not pass silently as complete.
func TestFixedListWarnsOnNextPageToken(t *testing.T) {
	for name, cmd := range fixedListCommands(t) {
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				label    string
				body     string
				flags    map[string]string
				wantRows int
				wantWarn bool
			}{
				{"token warns", `{"list":[{"id":"a"},{"id":"b"}],"nextPageToken":"more"}`, nil, 2, true},
				{"empty token silent", `{"list":[{"id":"a"},{"id":"b"}],"nextPageToken":""}`, nil, 2, false},
				{"absent token silent", `{"list":[{"id":"a"}]}`, nil, 1, false},
				// --limit satisfied inside the page: the caller got what was asked for.
				{"limit reached silent", `{"list":[{"id":"a"},{"id":"b"}],"nextPageToken":"more"}`, map[string]string{"limit": "1"}, 1, false},
			} {
				t.Run(tc.label, func(t *testing.T) {
					out, errOut := runFixedList(t, cmd, tc.body, tc.flags)
					if got := strings.Count(out, "\n"); got != tc.wantRows {
						t.Errorf("rows = %d, want %d; stdout = %q", got, tc.wantRows, out)
					}
					if got := strings.Contains(errOut, fixedListWarning); got != tc.wantWarn {
						t.Errorf("warning on stderr = %v, want %v; stderr = %q", got, tc.wantWarn, errOut)
					}
					if strings.Contains(out, fixedListWarning) {
						t.Errorf("warning leaked to stdout: %q", out)
					}
				})
			}
		})
	}
}
