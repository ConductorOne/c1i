package cmd

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// resetAutomationsListCmdFlags restores automationsListCmd's own flags to
// their zero values and clears pflag's per-flag Changed bit, so tests
// sharing the package-level singleton command can't leak flag state into
// each other. Mirrors resetPoliciesListCmdFlags in
// cmd/policies_pagination_test.go.
func resetAutomationsListCmdFlags(t *testing.T) {
	t.Helper()
	reset := func() {
		for name, def := range map[string]string{
			"page-size":      "50",
			"page-token":     "",
			"limit":          "0",
			"enabled-only":   "false",
			"calls-function": "",
		} {
			f := automationsListCmd.Flags().Lookup(name)
			_ = f.Value.Set(def)
			f.Changed = false
		}
	}
	reset()
	t.Cleanup(reset)
}

// runAutomationsListCmd drives automationsListCmd.RunE directly against a
// test server and returns its NDJSON stdout plus anything written to
// stderr (warnings).
func runAutomationsListCmd(t *testing.T, srv *httptest.Server) (stdout, stderr string, err error) {
	t.Helper()
	orig := newListClient
	newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newListClient = orig })
	t.Setenv("C1I_URL", "https://example.invalid")

	var out, errOut bytes.Buffer
	automationsListCmd.SetOut(&out)
	automationsListCmd.SetErr(&errOut)
	automationsListCmd.SetContext(context.Background())
	runErr := automationsListCmd.RunE(automationsListCmd, []string{})
	return out.String(), errOut.String(), runErr
}

// automationPage builds a GET /api/v1/automations response body holding the
// given ids with the given nextPageToken, matching the "automations list"
// case's flatPage("list","id") shape in cmd/list_pagination_test.go.
func automationPage(ids []string, nextToken string) string {
	items := make([]string, 0, len(ids))
	for _, id := range ids {
		items = append(items, fmt.Sprintf(`{"id":%q}`, id))
	}
	return fmt.Sprintf(`{"list":[%s],"nextPageToken":%q}`, joinJSON(items), nextToken)
}

func joinJSON(items []string) string {
	s := ""
	for i, it := range items {
		if i > 0 {
			s += ","
		}
		s += it
	}
	return s
}

// TestAutomationsListStuckCursorSameTokenTerminates reproduces the live
// defect: a server whose final page carries a non-empty nextPageToken that,
// when followed, returns the SAME page (including the same token) again.
// Without a duplicate-page guard this loops forever, re-emitting the same
// rows; the fix must stop after the repeat is detected and emit each row
// exactly once.
func TestAutomationsListStuckCursorSameTokenTerminates(t *testing.T) {
	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount > 4 {
			t.Fatalf("server received a 5th request; auto-pagination did not terminate")
		}
		tok := r.URL.Query().Get("page_token")
		w.Header().Set("Content-Type", "application/json")
		switch tok {
		case "":
			_, _ = fmt.Fprint(w, automationPage([]string{"auto-a", "auto-b"}, "stuck-tok"))
		case "stuck-tok":
			// The final page: same rows, same cursor, forever.
			_, _ = fmt.Fprint(w, automationPage([]string{"auto-a", "auto-b"}, "stuck-tok"))
		default:
			t.Errorf("unexpected page_token: %q", tok)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	resetAutomationsListCmdFlags(t)
	out, errOut, err := runAutomationsListCmd(t, srv)
	if err == nil {
		t.Fatalf("RunE: want an error (stuck cursor -> exit 6), got nil; stdout=%q stderr=%q", out, errOut)
	}
	if got, want := exitCode(err), exitServer; got != want {
		t.Errorf("exitCode(err) = %d, want %d (exitServer, \"C1 failed\"); err=%v", got, want, err)
	}

	rows := decodeNDJSONRows(t, out)
	if len(rows) != 2 || rows[0]["id"] != "auto-a" || rows[1]["id"] != "auto-b" {
		t.Fatalf("got rows %v, want exactly one occurrence each of auto-a, auto-b (a stuck cursor must not re-emit them)", rows)
	}
	if requestCount != 2 {
		t.Fatalf("server received %d requests, want exactly 2 (one to discover the stuck page, one to confirm it repeats)", requestCount)
	}
	if !strings.Contains(errOut, "INCOMPLETE") || !strings.Contains(errOut, "GET /api/v1/automations") {
		t.Errorf("stderr warning = %q, want it to say results are INCOMPLETE and name GET /api/v1/automations", errOut)
	}
}

// TestAutomationsListStuckCursorChangingTokenTerminates covers the case
// where the server's cursor string changes on every call (so a bare
// seen-token check never fires) but the page content never advances. The
// guard must key off duplicate item ids, not just the literal token value.
func TestAutomationsListStuckCursorChangingTokenTerminates(t *testing.T) {
	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount > 4 {
			t.Fatalf("server received a 5th request; auto-pagination did not terminate")
		}
		tok := r.URL.Query().Get("page_token")
		w.Header().Set("Content-Type", "application/json")
		switch tok {
		case "":
			_, _ = fmt.Fprint(w, automationPage([]string{"auto-a", "auto-b"}, "tok-v1"))
		case "tok-v1":
			// Same rows again, but a freshly minted, never-before-seen token.
			_, _ = fmt.Fprint(w, automationPage([]string{"auto-a", "auto-b"}, "tok-v2"))
		default:
			t.Errorf("unexpected page_token: %q", tok)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	resetAutomationsListCmdFlags(t)
	out, errOut, err := runAutomationsListCmd(t, srv)
	if err == nil {
		t.Fatalf("RunE: want an error (stuck cursor -> exit 6), got nil; stdout=%q stderr=%q", out, errOut)
	}
	if got, want := exitCode(err), exitServer; got != want {
		t.Errorf("exitCode(err) = %d, want %d (exitServer, \"C1 failed\"); err=%v", got, want, err)
	}

	rows := decodeNDJSONRows(t, out)
	if len(rows) != 2 || rows[0]["id"] != "auto-a" || rows[1]["id"] != "auto-b" {
		t.Fatalf("got rows %v, want exactly one occurrence each of auto-a, auto-b", rows)
	}
	if requestCount != 2 {
		t.Fatalf("server received %d requests, want exactly 2", requestCount)
	}
	if !strings.Contains(errOut, "INCOMPLETE") || !strings.Contains(errOut, "GET /api/v1/automations") {
		t.Errorf("stderr warning = %q, want it to say results are INCOMPLETE and name GET /api/v1/automations", errOut)
	}
}

// TestAutomationsListStuckCursorEmptyPagesTerminates covers a server that
// never advances past an empty final page but keeps returning the same
// cursor: the duplicate-id check never fires (empty pages are never a
// duplicate, matching TestListCommandsEmptyPageWithTokenDoesNotStopEarly),
// so the repeated-token guard must be the one to stop it.
func TestAutomationsListStuckCursorEmptyPagesTerminates(t *testing.T) {
	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount > 4 {
			t.Fatalf("server received a 5th request; auto-pagination did not terminate")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, automationPage(nil, "forever-tok"))
	}))
	defer srv.Close()

	resetAutomationsListCmdFlags(t)
	out, errOut, err := runAutomationsListCmd(t, srv)
	if err == nil {
		t.Fatalf("RunE: want an error (stuck cursor -> exit 6), got nil; stdout=%q stderr=%q", out, errOut)
	}
	if got, want := exitCode(err), exitServer; got != want {
		t.Errorf("exitCode(err) = %d, want %d (exitServer, \"C1 failed\"); err=%v", got, want, err)
	}

	rows := decodeNDJSONRows(t, out)
	if len(rows) != 0 {
		t.Fatalf("got %d rows, want 0", len(rows))
	}
	if requestCount != 2 {
		t.Fatalf("server received %d requests, want exactly 2 (the token repeats on the 2nd response, caught before a 3rd request)", requestCount)
	}
	if !strings.Contains(errOut, "INCOMPLETE") || !strings.Contains(errOut, "GET /api/v1/automations") {
		t.Errorf("stderr warning = %q, want it to say results are INCOMPLETE and name GET /api/v1/automations", errOut)
	}
}
