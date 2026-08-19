package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// TestExtractListAndTokenList pins the canonical case: endpoints that wrap
// items under "list" (users, apps, entitlements, functions, automations) keep
// working without --list-key.
func TestExtractListAndTokenList(t *testing.T) {
	data := []byte(`{"list":[{"id":"1"},{"id":"2"}],"nextPageToken":"abc"}`)
	items, token, err := extractListAndToken(data, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if token != "abc" {
		t.Errorf("token = %q, want abc", token)
	}
}

// TestExtractListAndTokenTypedKey pins the bug fix: endpoints like
// /api/v1/automation_executions use a typed key ("automationExecutions") for
// the array. Before this fix, c1i looked only for "list", got 0 items, and
// looped forever on the nextPageToken.
func TestExtractListAndTokenTypedKey(t *testing.T) {
	cases := map[string]string{
		"automationExecutions": `{"automationExecutions":[{"id":"e1"},{"id":"e2"}],"nextPageToken":"t"}`,
		"automations":          `{"automations":[{"id":"a1"}],"nextPageToken":""}`,
		"items":                `{"items":[{"id":"i1"},{"id":"i2"},{"id":"i3"}]}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			items, _, err := extractListAndToken([]byte(payload), "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(items) == 0 {
				t.Fatalf("expected non-empty list, got 0 items")
			}
		})
	}
}

// TestExtractListAndTokenForceKey pins the --list-key override. If the user
// specifies a key explicitly, we use that field even when other arrays exist.
func TestExtractListAndTokenForceKey(t *testing.T) {
	data := []byte(`{"list":[{"id":"primary"}],"extra":[{"id":"other"}],"nextPageToken":""}`)
	items, _, err := extractListAndToken(data, "extra")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	var item map[string]string
	_ = json.Unmarshal(items[0], &item)
	if item["id"] != "other" {
		t.Errorf("got id=%q, want 'other'", item["id"])
	}
}

// TestExtractListAndTokenForceKeyMissing returns an empty list (not an error)
// when --list-key points at a field that doesn't exist. The page itself is
// well-formed; the caller can decide whether absence means "stop" or "skip".
func TestExtractListAndTokenForceKeyMissing(t *testing.T) {
	data := []byte(`{"list":[{"id":"a"}],"nextPageToken":""}`)
	items, _, err := extractListAndToken(data, "nope")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected empty list, got %d items", len(items))
	}
}

// TestExtractListAndTokenForceKeyNotArray errors when the forced key exists
// but isn't an array — that's a user mistake worth surfacing rather than
// silently treating as empty.
func TestExtractListAndTokenForceKeyNotArray(t *testing.T) {
	data := []byte(`{"foo":"bar","nextPageToken":""}`)
	_, _, err := extractListAndToken(data, "foo")
	if err == nil {
		t.Fatal("expected error for non-array forced key, got nil")
	}
}

// TestExtractListAndTokenNoArray returns no items and no error when the
// response is a single-object endpoint (e.g. get-by-id). The api command
// shouldn't be paginating these in the first place, but the helper should
// not panic.
func TestExtractListAndTokenNoArray(t *testing.T) {
	data := []byte(`{"id":"123","displayName":"x"}`)
	items, token, err := extractListAndToken(data, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
	if token != "" {
		t.Errorf("expected empty token, got %q", token)
	}
}

// TestExtractListAndTokenNullToken handles the "nextPageToken": null case
// some endpoints emit instead of omitting the field.
func TestExtractListAndTokenNullToken(t *testing.T) {
	data := []byte(`{"list":[{"id":"1"}],"nextPageToken":null}`)
	_, token, err := extractListAndToken(data, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "" {
		t.Errorf("expected empty token, got %q", token)
	}
}

// TestExtractListAndTokenMultiArrayDeterministic pins that when a response
// carries more than one array-valued field (and no "list"), the chosen array
// is the first by sorted key name — not a randomized map-iteration pick. The
// loop below runs the extraction many times; a non-deterministic walk would
// eventually select "zebra".
func TestExtractListAndTokenMultiArrayDeterministic(t *testing.T) {
	data := []byte(`{"alpha":[{"id":"a"}],"zebra":[{"id":"z"}],"nextPageToken":"t"}`)
	for i := 0; i < 50; i++ {
		items, token, err := extractListAndToken(data, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "t" {
			t.Errorf("expected token %q, got %q", "t", token)
		}
		if len(items) != 1 || string(items[0]) != `{"id":"a"}` {
			t.Fatalf("expected the sorted-first array (alpha), got %v", items)
		}
	}
}

// --- --allow-delete-body opt-in tests ---
//
// These drive the real apiCmd.RunE (via ExecuteContext), not just a helper
// function, because the behavior under test is command-level wiring: a flag
// gating a guard, and that guard's effect on what reaches the wire. They
// substitute newAPIClient (a var exactly so tests can do this) with a fake
// that performs a genuine HTTP round trip against an httptest.Server,
// bypassing newClient's real OAuth mint, so the wire assertions are real.

// resetAPICmdFlags restores apiCmd's own flags to their zero values before a
// test drives it, and again afterward, so tests sharing the package-level
// apiCmd singleton can't leak flag state into each other or into other test
// files.
func resetAPICmdFlags(t *testing.T) {
	t.Helper()
	reset := func() {
		_ = apiCmd.Flags().Set("path", "")
		_ = apiCmd.Flags().Set("method", "")
		_ = apiCmd.Flags().Set("body", "")
		_ = apiCmd.Flags().Set("body-file", "")
		_ = apiCmd.Flags().Set("allow-delete-body", "false")
	}
	reset()
	t.Cleanup(reset)
}

// fakeWireRequester implements apiRequester by making a real HTTP request to
// an httptest.Server, so a test asserting on what the server received is
// asserting on an actual wire round trip, not an in-memory call record.
type fakeWireRequester struct {
	base string
	hc   *http.Client
}

func (f *fakeWireRequester) Request(ctx context.Context, method, path string, body []byte, headers map[string]string) ([]byte, error) {
	var rdr io.Reader
	if len(body) > 0 {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, f.base+path, rdr)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := f.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(resp.Body)
}

// stubNewAPIClient swaps newAPIClient to return a fakeWireRequester pointed
// at srv, restoring the original (real, OAuth-backed) implementation when
// the test ends.
func stubNewAPIClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	orig := newAPIClient
	newAPIClient = func(_ *cobra.Command, baseURL string) (apiRequester, error) {
		return &fakeWireRequester{base: baseURL, hc: srv.Client()}, nil
	}
	t.Cleanup(func() { newAPIClient = orig })
}

// TestAPIDeleteBodyRefusedByDefault is a regression guard: without
// --allow-delete-body, `api --method DELETE --body` must keep failing with
// the same refusal and the same (unclassified, exit 1) exit code it always
// has — this is the safety rail the opt-in is not allowed to weaken. It
// fails if the guard is ever dropped or the opt-in is made the default.
func TestAPIDeleteBodyRefusedByDefault(t *testing.T) {
	resetAPICmdFlags(t)
	t.Setenv("C1I_URL", "https://example.invalid")

	var out bytes.Buffer
	apiCmd.SetOut(&out)
	apiCmd.SetErr(&out)
	rootCmd.SetArgs([]string{
		"api",
		"--path", "/api/v1/memberships/x",
		"--method", "DELETE",
		"--body", `{"a":1}`,
	})

	err := rootCmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "--method DELETE does not take a request body") {
		t.Errorf("error = %q, want it to still name the refusal", err.Error())
	}
	if !strings.Contains(err.Error(), "--allow-delete-body") {
		t.Errorf("error = %q, want it to name the opt-in", err.Error())
	}
	if got, want := exitCode(err), exitError; got != want {
		t.Errorf("exitCode = %d, want %d (unclassified, same as before the opt-in existed)", got, want)
	}
}

// TestAPIDeleteBodyOptInSendsBodyOnWire proves --allow-delete-body actually
// carries the body to the wire: an httptest.Server records the method and
// body it received, and the test asserts both exactly, not just "no error".
func TestAPIDeleteBodyOptInSendsBodyOnWire(t *testing.T) {
	const wantBody = `{"reason":"cleanup"}`

	var gotMethod string
	var gotBody []byte
	// NewTLSServer, not NewServer: internal/config.ParseURL unconditionally
	// coerces the resolved base URL to "https://", so the fake requester (via
	// GetBaseURL) always dials https regardless of what scheme C1I_URL used.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = b
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	resetAPICmdFlags(t)
	stubNewAPIClient(t, srv)
	t.Setenv("C1I_URL", srv.URL)

	var out bytes.Buffer
	apiCmd.SetOut(&out)
	apiCmd.SetErr(&out)
	rootCmd.SetArgs([]string{
		"api",
		"--path", "/api/v1/memberships/zz-c1i-e2e-test/remove",
		"--method", "DELETE",
		"--body", wantBody,
		"--allow-delete-body",
	})

	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext: %v", err)
	}

	if gotMethod != http.MethodDelete {
		t.Errorf("server saw method %q, want DELETE", gotMethod)
	}
	if string(gotBody) != wantBody {
		t.Errorf("server saw body %q, want %q", gotBody, wantBody)
	}
}

// TestAPIDeleteBodyOptInDryRunPreviewsWithoutSending checks that --dry-run
// combined with --allow-delete-body previews the method, path, and body —
// and, just as importantly, that no request reaches the server at all.
func TestAPIDeleteBodyOptInDryRunPreviewsWithoutSending(t *testing.T) {
	const wantBody = `{"reason":"cleanup"}`

	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	resetAPICmdFlags(t)
	stubNewAPIClient(t, srv)
	t.Setenv("C1I_URL", srv.URL)

	origDryRun := viper.GetBool("dry_run")
	viper.Set("dry_run", true)
	t.Cleanup(func() { viper.Set("dry_run", origDryRun) })

	var out bytes.Buffer
	apiCmd.SetOut(&out)
	apiCmd.SetErr(&out)
	rootCmd.SetArgs([]string{
		"api",
		"--path", "/api/v1/memberships/zz-c1i-e2e-test/remove",
		"--method", "DELETE",
		"--body", wantBody,
		"--allow-delete-body",
	})

	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext: %v", err)
	}

	if called {
		t.Fatal("--dry-run sent a request to the server; it must not")
	}

	previewed := out.String()
	if !strings.Contains(previewed, "DELETE") || !strings.Contains(previewed, "/api/v1/memberships/zz-c1i-e2e-test/remove") {
		t.Errorf("dry-run preview = %q, want it to name method and path", previewed)
	}
	if !strings.Contains(previewed, "cleanup") {
		t.Errorf("dry-run preview = %q, want it to include the body", previewed)
	}
}
