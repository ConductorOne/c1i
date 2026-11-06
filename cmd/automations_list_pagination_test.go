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

// runAutomationsListCmd runs automationsListCmd against srv.
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

// automationPage builds a GET /api/v1/automations response body.
func automationPage(ids []string, nextToken string) string {
	items := make([]string, 0, len(ids))
	for _, id := range ids {
		items = append(items, fmt.Sprintf(`{"id":%q}`, id))
	}
	return fmt.Sprintf(`{"list":[%s],"nextPageToken":%q}`, strings.Join(items, ","), nextToken)
}

// A page that returns its own token again must stop without re-emitting rows.
func TestAutomationsListStuckCursorSameTokenTerminates(t *testing.T) {
	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount > 4 {
			t.Errorf("server received a 5th request; auto-pagination did not terminate")
			w.WriteHeader(http.StatusBadRequest)
			return
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

	resetCmdFlags(t, automationsListCmd)
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

// A fresh token every call over an unchanged page: the repeated page, not the
// token, must trip the guard.
func TestAutomationsListStuckCursorChangingTokenTerminates(t *testing.T) {
	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount > 4 {
			t.Errorf("server received a 5th request; auto-pagination did not terminate")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		tok := r.URL.Query().Get("page_token")
		w.Header().Set("Content-Type", "application/json")
		switch tok {
		case "":
			_, _ = fmt.Fprint(w, automationPage([]string{"auto-a", "auto-b"}, "tok-v1"))
		case "tok-v1", "tok-v2":
			// Same rows again, but a freshly minted, never-before-seen token.
			_, _ = fmt.Fprint(w, automationPage([]string{"auto-a", "auto-b"}, fmt.Sprintf("tok-v%d", requestCount)))
		default:
			t.Errorf("unexpected page_token: %q", tok)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	resetCmdFlags(t, automationsListCmd)
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
	if requestCount != 3 {
		t.Fatalf("server received %d requests, want exactly 3 (the page must repeat twice before the cursor counts as stuck)", requestCount)
	}
	if !strings.Contains(errOut, "INCOMPLETE") || !strings.Contains(errOut, "GET /api/v1/automations") {
		t.Errorf("stderr warning = %q, want it to say results are INCOMPLETE and name GET /api/v1/automations", errOut)
	}
}

// Empty pages never count as repeats, so a repeated token over them must be
// what stops the loop.
func TestAutomationsListStuckCursorEmptyPagesTerminates(t *testing.T) {
	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount > 4 {
			t.Errorf("server received a 5th request; auto-pagination did not terminate")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, automationPage(nil, "forever-tok"))
	}))
	defer srv.Close()

	resetCmdFlags(t, automationsListCmd)
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
