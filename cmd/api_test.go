package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
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

// resetAPICmdFlags restores apiCmd's flags -- including the persistent ones
// cobra merges in -- to their defaults before and after a test, so tests
// sharing the package-level singleton cannot leak flag state.
func resetAPICmdFlags(t *testing.T) {
	t.Helper()
	// Shared so the two traps are handled in one place: Set appends on a
	// StringArray, and a cleared-but-Changed flag reads as "set to nothing".
	resetCmdFlags(t, apiCmd)
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
// the same refusal, still naming the opt-in — that invariant is the safety
// rail, and this test fails if the guard is ever dropped or the opt-in is
// made the default. The exit code is classified as exitUsage (2): this is a
// malformed invocation (bad flag combination), not an unclassified failure,
// so exitError (1) was never the "correct" code — it was a pre-existing gap.
// See TestAPIGetBodyRefused for the GET-with-body twin.
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
	if got, want := exitCode(err), exitUsage; got != want {
		t.Errorf("exitCode = %d, want %d (usage error: bad flag combination)", got, want)
	}
}

// TestAPIGetBodyRefused is the GET twin of TestAPIDeleteBodyRefusedByDefault:
// GET never takes a body (there's no opt-in for it, unlike DELETE), and the
// refusal must classify as exitUsage (2), not the generic exitError (1) it
// returned before this guard was wrapped in usageError.
func TestAPIGetBodyRefused(t *testing.T) {
	resetAPICmdFlags(t)
	t.Setenv("C1I_URL", "https://example.invalid")

	var out bytes.Buffer
	apiCmd.SetOut(&out)
	apiCmd.SetErr(&out)
	rootCmd.SetArgs([]string{
		"api",
		"--path", "/api/v1/apps",
		"--method", "GET",
		"--body", `{"a":1}`,
	})

	err := rootCmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "--method GET does not take a request body") {
		t.Errorf("error = %q, want it to still name the refusal", err.Error())
	}
	if got, want := exitCode(err), exitUsage; got != want {
		t.Errorf("exitCode = %d, want %d (usage error: bad flag combination)", got, want)
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
	// srv is a plain-http httptest.Server, but stubNewAPIClient bypasses
	// GetBaseURL's return value entirely (it points the real client straight
	// at srv.URL) -- this only needs to satisfy GetBaseURL's now-https-only
	// parse, not name the real target.
	t.Setenv("C1I_URL", "https://example.invalid")

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

// --- `api --paginate` + --fields zero-match end-to-end ---
//
// twoPageFixtureServer serves a two-page paginated list over POST (matching
// what `api --body "{}"` sends: --body implies POST, and the api command
// splices the cursor into the request body's "pageToken" field for
// POST/PUT/PATCH, not a query param — that's GET/DELETE's mechanism only).
// A request with no "pageToken" (or "") in its body returns page 1 with
// nextPageToken "p2"; a request with pageToken "p2" returns page 2 with no
// nextPageToken, ending pagination. Each page's item shape is supplied by
// the caller, so the same fixture can produce rows that either all miss
// --fields or split across pages.
//
// NewTLSServer, not NewServer: internal/config.ParseURL unconditionally
// coerces the resolved base URL to "https://" (see
// TestAPIDeleteBodyOptInSendsBodyOnWire above), so the fake requester always
// dials https regardless of what scheme C1I_URL used.
func twoPageFixtureServer(page1Item, page2Item string) *httptest.Server {
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			PageToken string `json:"pageToken"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)

		w.Header().Set("Content-Type", "application/json")
		if body.PageToken == "p2" {
			_, _ = w.Write([]byte(`{"list":[` + page2Item + `],"nextPageToken":""}`))
			return
		}
		_, _ = w.Write([]byte(`{"list":[` + page1Item + `],"nextPageToken":"p2"}`))
	}))
}

// TestAPIPaginateFieldsZeroMatchAcrossAllPagesErrors is the `api --paginate`
// half of the fix: a --fields spec that matches nothing in EITHER page's row
// projects every row to "{}", so both are skipped (streaming, never
// buffered — --paginate exists precisely to walk unbounded results without
// holding them all in memory) and stdout ends up completely empty, then the
// command fails with exit 2 — not the pre-fix behavior of silently exiting 0
// having printed two "{}" rows (confirmed live against a live tenant before
// this fix).
func TestAPIPaginateFieldsZeroMatchAcrossAllPagesErrors(t *testing.T) {
	srv := twoPageFixtureServer(`{"name":"page1-item"}`, `{"name":"page2-item"}`)
	defer srv.Close()

	resetAPICmdFlags(t)
	stubNewAPIClient(t, srv)
	t.Setenv("C1I_URL", srv.URL)
	viper.Set("fields", "id")
	t.Cleanup(func() { viper.Set("fields", "") })

	var out bytes.Buffer
	apiCmd.SetOut(&out)
	apiCmd.SetErr(&out)
	rootCmd.SetArgs([]string{
		"api",
		"--path", "/api/v1/search/things",
		"--body", "{}",
		"--paginate",
	})

	err := rootCmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatalf("expected an error (exit 2) when --fields matches nothing on any page; output: %s", out.String())
	}
	var usageErr *usageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("error = %v (%T), want *usageError (exit code 2)", err, err)
	}
	if got, want := exitCode(err), exitUsage; got != want {
		t.Errorf("exitCode = %d, want %d", got, want)
	}
	if !strings.Contains(err.Error(), "matched no keys in any row of the response") {
		t.Errorf("error = %q, want the list-specific zero-match message", err.Error())
	}
	if out.Len() != 0 {
		t.Errorf("output = %q, want completely empty stdout (both pages' rows projected to {} and must be skipped)", out.String())
	}
}

// TestAPIPaginateFieldsPartialMatchAcrossPagesSucceeds is the sparse-data
// twin, split across pages instead of rows within one page: page 1's item
// lacks "id", page 2's item has it. A match anywhere in the whole paginated
// result — not just the first page — must be enough for exit 0, and page 1's
// empty projection must be skipped rather than written as "{}".
func TestAPIPaginateFieldsPartialMatchAcrossPagesSucceeds(t *testing.T) {
	srv := twoPageFixtureServer(`{"name":"page1-item"}`, `{"id":"only-on-page-2"}`)
	defer srv.Close()

	resetAPICmdFlags(t)
	stubNewAPIClient(t, srv)
	t.Setenv("C1I_URL", srv.URL)
	viper.Set("fields", "id")
	t.Cleanup(func() { viper.Set("fields", "") })

	var out bytes.Buffer
	apiCmd.SetOut(&out)
	apiCmd.SetErr(&out)
	rootCmd.SetArgs([]string{
		"api",
		"--path", "/api/v1/search/things",
		"--body", "{}",
		"--paginate",
	})

	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("expected exit 0 (page 2 matched --fields), got error: %v; output: %s", err, out.String())
	}
	gotLines := strings.Split(strings.TrimSpace(out.String()), "\n")
	wantLines := []string{`{"id":"only-on-page-2"}`}
	if !reflect.DeepEqual(gotLines, wantLines) {
		t.Errorf("output lines = %v, want %v (page 1's empty projection must be skipped, not written as {})", gotLines, wantLines)
	}
}

// TestAPIPaginateLimitWithSparseFieldsContinuesPastFilteredPage catches a
// regression the empty-row-skip fix (above) introduced: --limit is compared
// against a plain per-item counter incremented every time enc.Encode is
// CALLED, not every time it actually WRITES. Once Encode started silently
// skipping empty-projection rows, "called" and "written" diverged — a
// --limit that should count WRITTEN rows (addLimitFlag's documented
// contract) was actually counting SCANNED rows, so a filtered-out row could
// trip the limit and stop pagination before a real match on a later page
// was ever reached.
//
// Page 1's item misses --fields entirely (projects to {}, skipped); page 2's
// item matches. With --limit 1, the fix must still fetch page 2 and write
// exactly one line, not stop after page 1's single scanned-but-unwritten
// item.
func TestAPIPaginateLimitWithSparseFieldsContinuesPastFilteredPage(t *testing.T) {
	var requestCount int
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		var body struct {
			PageToken string `json:"pageToken"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)

		w.Header().Set("Content-Type", "application/json")
		if body.PageToken == "p2" {
			_, _ = w.Write([]byte(`{"list":[{"id":"only-on-page-2"}],"nextPageToken":""}`))
			return
		}
		_, _ = w.Write([]byte(`{"list":[{"name":"page1-item-no-id"}],"nextPageToken":"p2"}`))
	}))
	defer srv.Close()

	resetAPICmdFlags(t)
	stubNewAPIClient(t, srv)
	t.Setenv("C1I_URL", srv.URL)
	viper.Set("fields", "id")
	t.Cleanup(func() { viper.Set("fields", "") })

	var out bytes.Buffer
	apiCmd.SetOut(&out)
	apiCmd.SetErr(&out)
	rootCmd.SetArgs([]string{
		"api",
		"--path", "/api/v1/search/things",
		"--body", "{}",
		"--paginate",
		"--limit", "1",
	})

	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("expected exit 0 (page 2's row matched --fields), got error: %v; output: %s", err, out.String())
	}
	if requestCount != 2 {
		t.Fatalf("server received %d requests, want 2 (page 1's filtered-out row must not stop pagination before page 2 is fetched)", requestCount)
	}
	gotLines := strings.Split(strings.TrimSpace(out.String()), "\n")
	wantLines := []string{`{"id":"only-on-page-2"}`}
	if !reflect.DeepEqual(gotLines, wantLines) {
		t.Errorf("output lines = %v, want %v (exactly --limit 1 WRITTEN row, not 1 scanned row)", gotLines, wantLines)
	}
}

// --- --path without a leading slash (Fix 3) ---

// TestAPIPathWithoutLeadingSlashIsUsageError pins the fix: a --path missing
// its leading slash used to get concatenated straight onto baseURL (e.g.
// "https://host" + "api/v1/users" -> a DNS lookup for "hostapi.v1..."),
// surfacing as an opaque "no such host" error that never named the real
// problem. It must now fail fast as a usage error naming --path itself.
func TestAPIPathWithoutLeadingSlashIsUsageError(t *testing.T) {
	resetAPICmdFlags(t)
	t.Setenv("C1I_URL", "https://example.invalid")

	var out bytes.Buffer
	apiCmd.SetOut(&out)
	apiCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"api", "--path", "api/v1/users"})

	err := rootCmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected an error for a --path without a leading slash, got nil")
	}
	if !strings.Contains(err.Error(), "--path") || !strings.Contains(err.Error(), "leading slash") {
		t.Errorf("error = %q, want it to name --path and the leading-slash requirement", err.Error())
	}
	if got, want := exitCode(err), exitUsage; got != want {
		t.Errorf("exitCode = %d, want %d (usage error: bad --path)", got, want)
	}
}

// TestAPIPathWithLeadingSlashStillWorks is the regression guard: a normal,
// well-formed --path must be unaffected.
func TestAPIPathWithLeadingSlashStillWorks(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users" {
			t.Errorf("server saw path %q, want /api/v1/users", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"list":[]}`))
	}))
	defer srv.Close()

	resetAPICmdFlags(t)
	stubNewAPIClient(t, srv)
	t.Setenv("C1I_URL", srv.URL)

	var out bytes.Buffer
	apiCmd.SetOut(&out)
	apiCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"api", "--path", "/api/v1/users"})

	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("expected exit 0 for a well-formed --path, got: %v; output: %s", err, out.String())
	}
}

// --- non-JSON 200 body (Fix 1) ---
//
// `api --path "/api/v1/../../etc/passwd"` (or any path that escapes the API
// prefix) can land on a server that answers 200 with an HTML document
// instead of the JSON api documents itself as returning. Before the fix,
// writeRawObject's json.Indent failure fell back to printing the raw bytes
// and returning nil -- a silent "success" a downstream parser can't detect.

// TestAPINonJSON200BodyIsError is the red case: a 200 whose body isn't valid
// JSON must be a reported error, not a silent success.
func TestAPINonJSON200BodyIsError(t *testing.T) {
	// NewTLSServer, not NewServer: c1i requires https, so the fake requester
	// (via GetBaseURL) always dials https.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<!doctype html><html><body>not json</body></html>"))
	}))
	defer srv.Close()

	resetAPICmdFlags(t)
	stubNewAPIClient(t, srv)
	t.Setenv("C1I_URL", srv.URL)

	var out bytes.Buffer
	apiCmd.SetOut(&out)
	apiCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"api", "--path", "/api/v1/../../etc/passwd"})

	err := rootCmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatalf("expected an error for a non-JSON 200 body, got nil; output: %s", out.String())
	}
	if !strings.Contains(err.Error(), "not JSON") && !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("error = %q, want it to say the response wasn't JSON", err.Error())
	}
	if got, want := exitCode(err), exitServer; got != want {
		t.Errorf("exitCode = %d, want %d (remote responded 200 but not with usable JSON)", got, want)
	}
}

// TestAPIEmpty200BodySucceeds pins the case the fix must not break: some
// endpoints legitimately answer 2xx with an empty body (a delete with
// nothing to return). That must still be exit 0 with no output.
func TestAPIEmpty200BodySucceeds(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	resetAPICmdFlags(t)
	stubNewAPIClient(t, srv)
	t.Setenv("C1I_URL", srv.URL)

	var out bytes.Buffer
	apiCmd.SetOut(&out)
	apiCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"api", "--path", "/api/v1/things/1", "--method", "DELETE"})

	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("expected exit 0 for a legitimately empty 200 body, got: %v; output: %s", err, out.String())
	}
	if out.String() != "" {
		t.Errorf("expected no output for an empty body, got: %q", out.String())
	}
}

// TestAPINormalJSONBodyStillWorks is the ordinary case: a real JSON response
// must still print and exit 0.
func TestAPINormalJSONBodyStillWorks(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","name":"n"}`))
	}))
	defer srv.Close()

	resetAPICmdFlags(t)
	stubNewAPIClient(t, srv)
	t.Setenv("C1I_URL", srv.URL)

	var out bytes.Buffer
	apiCmd.SetOut(&out)
	apiCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"api", "--path", "/api/v1/things/1"})

	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("expected exit 0 for a normal JSON body, got: %v; output: %s", err, out.String())
	}
	var got map[string]any
	if jsonErr := json.Unmarshal(out.Bytes(), &got); jsonErr != nil {
		t.Fatalf("output not valid JSON: %v (%s)", jsonErr, out.String())
	}
}

// warningMarker is the stable substring of the truncated-page warning. Both
// tests below assert on it: one that it reaches stderr, one that it never
// reaches stdout.
const warningMarker = "discarding the cursor"

// TestAPITruncatedPageWarns covers the no---paginate states of the cursor
// warning: a non-empty nextPageToken is a silently partial result and must
// warn, while an empty or absent token is a complete result and must NOT (a
// warning there would fire on nearly every call and get tuned out).
//
// Every case also asserts the warning stays OFF stdout. That is the change's
// central safety claim -- anything parsing `c1i api` must be unaffected -- and
// without this assertion the suite stayed green when the warning was made to
// write to stdout as well.
func TestAPITruncatedPageWarns(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		wantWarn bool
	}{
		{name: "partial result warns", body: `{"list":[{"id":"a"}],"nextPageToken":"abc"}`, wantWarn: true},
		{name: "non-list object with token warns", body: `{"id":"one","nextPageToken":"abc"}`, wantWarn: true},
		{name: "last page silent", body: `{"list":[{"id":"a"}],"nextPageToken":""}`},
		{name: "no token field silent", body: `{"ok":true}`},
		{name: "null token silent", body: `{"list":[{"id":"a"}],"nextPageToken":null}`},
		{name: "non-string token silent", body: `{"list":[{"id":"a"}],"nextPageToken":123}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			resetAPICmdFlags(t)
			stubNewAPIClient(t, srv)
			t.Setenv("C1I_URL", srv.URL)

			var out, errOut bytes.Buffer
			apiCmd.SetOut(&out)
			apiCmd.SetErr(&errOut)
			rootCmd.SetArgs([]string{"api", "--path", "/api/v1/catalogs"})

			if err := rootCmd.ExecuteContext(context.Background()); err != nil {
				t.Fatalf("ExecuteContext: %v", err)
			}

			if got := strings.Contains(errOut.String(), warningMarker); got != tc.wantWarn {
				t.Errorf("warning on stderr = %v, want %v; stderr = %q", got, tc.wantWarn, errOut.String())
			}
			if strings.Contains(out.String(), warningMarker) {
				t.Errorf("warning leaked to stdout, which parsers consume; stdout = %q", out.String())
			}
			if out.Len() == 0 {
				t.Error("stdout is empty; the response body should still be written")
			}
		})
	}
}

// getTwoPageFixtureServer serves a two-page list over GET, advancing on the
// page_token QUERY parameter. Distinct from twoPageFixtureServer, which reads
// pageToken from a POST body: driving GET pagination against that one would
// resend page 1 forever and trip the stuck-cursor guard rather than testing
// what this test is about.
func getTwoPageFixtureServer() *httptest.Server {
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page_token") == "p2" {
			_, _ = w.Write([]byte(`{"list":[{"id":"b"}],"nextPageToken":""}`))
			return
		}
		_, _ = w.Write([]byte(`{"list":[{"id":"a"}],"nextPageToken":"p2"}`))
	}))
}

// TestAPIPaginateDrainsCursorWithoutWarning is the case a single-body fixture
// cannot express: --paginate against a genuinely multi-page endpoint must
// drain every page and stay silent, because it discards no cursor. Asserting
// this against a one-page fixture proves nothing -- with an empty token the
// warning branch is unreachable whether or not --paginate is set.
func TestAPIPaginateDrainsCursorWithoutWarning(t *testing.T) {
	srv := getTwoPageFixtureServer()
	defer srv.Close()

	resetAPICmdFlags(t)
	stubNewAPIClient(t, srv)
	t.Setenv("C1I_URL", srv.URL)

	var out, errOut bytes.Buffer
	apiCmd.SetOut(&out)
	apiCmd.SetErr(&errOut)
	rootCmd.SetArgs([]string{"api", "--path", "/api/v1/catalogs", "--paginate"})

	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext: %v", err)
	}

	if strings.Contains(errOut.String(), warningMarker) {
		t.Errorf("--paginate drains the cursor, so it must not warn; stderr = %q", errOut.String())
	}
	// Both pages, proving the cursor really was followed rather than the
	// warning merely being suppressed.
	for _, want := range []string{`"id":"a"`, `"id":"b"`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("stdout missing %s; got %q", want, out.String())
		}
	}
}

// A repeated nextPageToken must stop --paginate before re-fetching, emit the
// row once, warn naming the route, and exit 6.
func TestAPIPaginateSameTokenTwiceReturnsExitServer(t *testing.T) {
	var requestCount int
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount > 4 {
			t.Errorf("server received a 5th request; --paginate did not terminate")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"list":[{"id":"a"}],"nextPageToken":"stuck-tok"}`)
	}))
	defer srv.Close()

	resetAPICmdFlags(t)
	stubNewAPIClient(t, srv)
	t.Setenv("C1I_URL", srv.URL)

	var out, errOut bytes.Buffer
	apiCmd.SetOut(&out)
	apiCmd.SetErr(&errOut)
	rootCmd.SetArgs([]string{"api", "--path", "/api/v1/automations", "--paginate"})

	err := rootCmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatalf("ExecuteContext: want an error (stuck cursor -> exit 6), got nil; stdout=%q stderr=%q", out.String(), errOut.String())
	}
	if got, want := exitCode(err), exitServer; got != want {
		t.Errorf("exitCode(err) = %d, want %d (exitServer); err=%v", got, want, err)
	}
	if requestCount != 2 {
		t.Fatalf("server received %d requests, want exactly 2 (one to discover the stuck page, one to confirm it repeats)", requestCount)
	}
	rows := decodeNDJSONRows(t, out.String())
	if len(rows) != 1 || rows[0]["id"] != "a" {
		t.Fatalf("got rows %v, want exactly one occurrence of id=a", rows)
	}
	if !strings.Contains(errOut.String(), "INCOMPLETE") || !strings.Contains(errOut.String(), "GET /api/v1/automations") {
		t.Errorf("stderr = %q, want it to say results are INCOMPLETE and name GET /api/v1/automations", errOut.String())
	}
}

// A bare "id" need not be unique (an entitlement id repeats across apps), and
// identical id-less items are legitimate, so a page with any new identity is
// emitted whole.
func TestAPIPaginateKeepsItemsSharingAnIdentity(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page_token") == "" {
			_, _ = fmt.Fprint(w, `{"list":[{"id":"e1","appId":"a1"},{"id":"e1","appId":"a2"},{"x":1},{"x":1}],"nextPageToken":"tok1"}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"list":[{"id":"e1","appId":"a3"},{"id":"e2"}],"nextPageToken":""}`)
	}))
	defer srv.Close()

	resetAPICmdFlags(t)
	stubNewAPIClient(t, srv)
	t.Setenv("C1I_URL", srv.URL)

	var out, errOut bytes.Buffer
	apiCmd.SetOut(&out)
	apiCmd.SetErr(&errOut)
	rootCmd.SetArgs([]string{"api", "--path", "/api/v1/x", "--paginate"})
	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext: %v (stderr %q)", err, errOut.String())
	}
	if rows := decodeNDJSONRows(t, out.String()); len(rows) != 6 {
		t.Errorf("got %d rows, want all 6", len(rows))
	}
}

// A page whose identities all collide with earlier ones (an entitlement id
// reused across apps), or a final page identical to the one before, is real
// data to a raw passthrough and must be printed.
func TestAPIPaginatePrintsIsolatedAllSeenPage(t *testing.T) {
	cases := []struct {
		name  string
		pages []string
		want  int
	}{
		{"colliding ids then a new one", []string{`[{"id":"e1","appId":"a1"}]`, `[{"id":"e1","appId":"a2"}]`, `[{"id":"e2"}]`}, 3},
		{"identical id-less last page", []string{`[{"x":1}]`, `[{"x":1}]`}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var requests int
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				next := fmt.Sprintf("tok-%d", requests)
				if requests == len(tc.pages) {
					next = ""
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"list":%s,"nextPageToken":%q}`, tc.pages[requests-1], next)
			}))
			defer srv.Close()

			resetAPICmdFlags(t)
			stubNewAPIClient(t, srv)
			t.Setenv("C1I_URL", srv.URL)

			var out, errOut bytes.Buffer
			apiCmd.SetOut(&out)
			apiCmd.SetErr(&errOut)
			rootCmd.SetArgs([]string{"api", "--path", "/api/v1/x", "--paginate"})
			if err := rootCmd.ExecuteContext(context.Background()); err != nil {
				t.Fatalf("ExecuteContext: %v (stderr %q)", err, errOut.String())
			}
			if rows := decodeNDJSONRows(t, out.String()); len(rows) != tc.want {
				t.Errorf("got %d rows, want %d", len(rows), tc.want)
			}
		})
	}
}

// The observed defect: a fresh token every call over the same page. The
// identical page, not the token, must trip the guard.
func TestAPIPaginateChangingTokenSameRowsReturnsExitServer(t *testing.T) {
	var requestCount int
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount > 4 {
			t.Errorf("server received a 5th request; --paginate did not terminate")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		tok := r.URL.Query().Get("page_token")
		w.Header().Set("Content-Type", "application/json")
		switch tok {
		case "":
			_, _ = fmt.Fprintf(w, `{"list":%s,"nextPageToken":"tok-v1"}`, stalePageItems(`{"id":"a%d"}`))
		case "tok-v1", "tok-v2":
			_, _ = fmt.Fprintf(w, `{"list":%s,"nextPageToken":"tok-v%d"}`, stalePageItems(`{"id":"a%d"}`), requestCount)
		default:
			t.Errorf("unexpected page_token: %q", tok)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	resetAPICmdFlags(t)
	stubNewAPIClient(t, srv)
	t.Setenv("C1I_URL", srv.URL)

	var out, errOut bytes.Buffer
	apiCmd.SetOut(&out)
	apiCmd.SetErr(&errOut)
	rootCmd.SetArgs([]string{"api", "--path", "/api/v1/automations", "--paginate"})

	err := rootCmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatalf("ExecuteContext: want an error (stuck cursor -> exit 6), got nil; stdout=%q stderr=%q", out.String(), errOut.String())
	}
	if got, want := exitCode(err), exitServer; got != want {
		t.Errorf("exitCode(err) = %d, want %d (exitServer); err=%v", got, want, err)
	}
	if requestCount != 3 {
		t.Fatalf("server received %d requests, want exactly 3 (the page must repeat twice before the cursor counts as stuck)", requestCount)
	}
	rows := decodeNDJSONRows(t, out.String())
	if len(rows) != stuckStaleRows || rows[0]["id"] != "a0" {
		t.Fatalf("got %d rows, want the page's %d rows once each (no duplicate emission)", len(rows), stuckStaleRows)
	}
	if !strings.Contains(errOut.String(), "INCOMPLETE") || !strings.Contains(errOut.String(), "GET /api/v1/automations") {
		t.Errorf("stderr = %q, want it to say results are INCOMPLETE and name GET /api/v1/automations", errOut.String())
	}
}

// Items with no "id" are compared by canonical JSON, so a stuck page of them
// is still detected.
func TestAPIPaginateStuckCursorWithoutIDFallsBackToCanonicalJSON(t *testing.T) {
	var requestCount int
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount > 4 {
			t.Errorf("server received a 5th request; --paginate did not terminate")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		tok := r.URL.Query().Get("page_token")
		w.Header().Set("Content-Type", "application/json")
		switch tok {
		case "":
			_, _ = fmt.Fprintf(w, `{"list":%s,"nextPageToken":"tok-v1"}`, stalePageItems(`{"name":"n1","value":%d}`))
		case "tok-v1", "tok-v2":
			// Same logical items, same key order even -- a changing token is
			// the only thing that looks different.
			_, _ = fmt.Fprintf(w, `{"list":%s,"nextPageToken":"tok-v%d"}`, stalePageItems(`{"name":"n1","value":%d}`), requestCount)
		default:
			t.Errorf("unexpected page_token: %q", tok)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	resetAPICmdFlags(t)
	stubNewAPIClient(t, srv)
	t.Setenv("C1I_URL", srv.URL)

	var out, errOut bytes.Buffer
	apiCmd.SetOut(&out)
	apiCmd.SetErr(&errOut)
	rootCmd.SetArgs([]string{"api", "--path", "/api/v1/decoys", "--paginate"})

	err := rootCmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatalf("ExecuteContext: want an error (stuck cursor -> exit 6), got nil; stdout=%q stderr=%q", out.String(), errOut.String())
	}
	if got, want := exitCode(err), exitServer; got != want {
		t.Errorf("exitCode(err) = %d, want %d (exitServer); err=%v", got, want, err)
	}
	if requestCount != 3 {
		t.Fatalf("server received %d requests, want exactly 3 (the page must repeat twice before the cursor counts as stuck)", requestCount)
	}
	rows := decodeNDJSONRows(t, out.String())
	if len(rows) != stuckStaleRows || rows[0]["name"] != "n1" {
		t.Fatalf("got %d rows, want the page's %d rows once each (no duplicate emission)", len(rows), stuckStaleRows)
	}
}

// A healthy offset-paginated server that gains exactly one page of rows
// between requests repeats a page once. That must not be read as a stuck
// cursor: pagination continues and the run exits 0. api is a raw
// passthrough, so the repeat is printed rather than dropped.
func TestAPIPaginateSingleRepeatedPageIsNotStuck(t *testing.T) {
	var requestCount int
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		switch tok := r.URL.Query().Get("page_token"); tok {
		case "":
			_, _ = fmt.Fprint(w, `{"list":[{"id":"a"},{"id":"b"}],"nextPageToken":"t1"}`)
		case "t1":
			_, _ = fmt.Fprint(w, `{"list":[{"id":"a"},{"id":"b"}],"nextPageToken":"t2"}`)
		case "t2":
			_, _ = fmt.Fprint(w, `{"list":[{"id":"c"}],"nextPageToken":""}`)
		default:
			t.Errorf("unexpected page_token: %q", tok)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	resetAPICmdFlags(t)
	stubNewAPIClient(t, srv)
	t.Setenv("C1I_URL", srv.URL)

	var out, errOut bytes.Buffer
	apiCmd.SetOut(&out)
	apiCmd.SetErr(&errOut)
	rootCmd.SetArgs([]string{"api", "--path", "/api/v1/things", "--paginate"})

	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext: %v (exit %d); stderr=%q", err, exitCode(err), errOut.String())
	}
	var got []string
	for _, row := range decodeNDJSONRows(t, out.String()) {
		got = append(got, fmt.Sprint(row["id"]))
	}
	if strings.Join(got, ",") != "a,b,a,b,c" {
		t.Fatalf("rows = %v, want [a b a b c] (the isolated repeat printed, not dropped)", got)
	}
	if requestCount != 3 {
		t.Fatalf("server received %d requests, want 3", requestCount)
	}
}

// Id-less items that differ only in an integer beyond float64 precision must
// keep distinct identities, or a legitimate page reads as a repeat.
func TestItemIdentityKeepsLargeIntegersDistinct(t *testing.T) {
	a := itemIdentity(json.RawMessage(`{"n":9007199254740993}`))
	b := itemIdentity(json.RawMessage(`{"n":9007199254740992}`))
	if a == b {
		t.Fatalf("itemIdentity collapsed distinct large integers to %q", a)
	}
	if x, y := itemIdentity(json.RawMessage(`{"b":1,"a":2}`)), itemIdentity(json.RawMessage(`{"a":2, "b":1}`)); x != y {
		t.Fatalf("itemIdentity differs for the same object with reordered keys: %q vs %q", x, y)
	}
}

// Stuck shapes that are not a single page repeated: each must stop with exit
// 6 and print every row once, never cycle (within --limit or without). With
// one-row pages the stale run needs stuckStaleRows pages, hence the high cap.
func TestAPIPaginateStuckCycleShapesReturnExitServer(t *testing.T) {
	cases := []struct {
		name  string
		pages []string // served in rotation, each under a fresh token
		limit string
		want  string
	}{
		{"alternating pages", []string{`[{"id":"a"}]`, `[{"id":"b"}]`}, "", "a,b"},
		{"alternating pages under --limit", []string{`[{"id":"a"}]`, `[{"id":"b"}]`}, "7", "a,b"},
		{"page then empty page", []string{`[{"id":"a"}]`, `[]`}, "", "a"},
		{"same rows reordered", []string{`[{"id":"a"},{"id":"b"}]`, `[{"id":"b"},{"id":"a"}]`}, "", "a,b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var requestCount int
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount++
				if requestCount > 100 {
					t.Errorf("server received a 101st request; --paginate did not terminate")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"list":%s,"nextPageToken":"tok-%d"}`, tc.pages[(requestCount-1)%len(tc.pages)], requestCount)
			}))
			defer srv.Close()

			resetAPICmdFlags(t)
			stubNewAPIClient(t, srv)
			t.Setenv("C1I_URL", srv.URL)

			var out, errOut bytes.Buffer
			apiCmd.SetOut(&out)
			apiCmd.SetErr(&errOut)
			args := []string{"api", "--path", "/api/v1/things", "--paginate"}
			if tc.limit != "" {
				args = append(args, "--limit", tc.limit)
			}
			rootCmd.SetArgs(args)

			err := rootCmd.ExecuteContext(context.Background())
			if got := exitCode(err); got != exitServer {
				t.Fatalf("exitCode = %d, want %d (exitServer); err=%v stderr=%q", got, exitServer, err, errOut.String())
			}
			var got []string
			for _, row := range decodeNDJSONRows(t, out.String()) {
				got = append(got, fmt.Sprint(row["id"]))
			}
			if strings.Join(got, ",") != tc.want {
				t.Fatalf("rows = %v, want %s", got, tc.want)
			}
		})
	}
}

// A --list-key absent from a page that has other arrays printed 0 rows at
// exit 0; it must be a usage error naming the key and the arrays present.
// A page with no arrays at all (protojson omits an empty repeated field)
// stays a clean empty result.
func TestAPIPaginateListKeyAbsent(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		wantErr    bool
	}{
		{"other arrays present", `{"list":[{"id":"a"}],"extra":[],"nextPageToken":""}`, true},
		{"no arrays", `{"nextPageToken":""}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()

			resetAPICmdFlags(t)
			stubNewAPIClient(t, srv)
			t.Setenv("C1I_URL", srv.URL)

			var out, errOut bytes.Buffer
			apiCmd.SetOut(&out)
			apiCmd.SetErr(&errOut)
			rootCmd.SetArgs([]string{"api", "--path", "/api/v1/automations", "--paginate", "--list-key", "nope"})

			err := rootCmd.ExecuteContext(context.Background())
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("ExecuteContext: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want a usage error, got nil; stdout=%q", out.String())
			}
			if got := exitCode(err); got != exitUsage {
				t.Errorf("exitCode = %d, want %d (usage); err=%v", got, exitUsage, err)
			}
			for _, want := range []string{`"nope"`, "extra, list"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %s", err.Error(), want)
				}
			}
		})
	}
}

// protojson omits an empty repeated field, so a later page can lack the
// --list-key while carrying another array. Once the key has been seen, that
// is the end of the list, not a misspelled key.
func TestAPIPaginateListKeyOmittedOnLaterPage(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page_token") == "" {
			_, _ = fmt.Fprint(w, `{"list":[{"id":"a"}],"nextPageToken":"tok2"}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"facets":[{"id":"f"}],"nextPageToken":""}`)
	}))
	defer srv.Close()

	resetAPICmdFlags(t)
	stubNewAPIClient(t, srv)
	t.Setenv("C1I_URL", srv.URL)

	var out, errOut bytes.Buffer
	apiCmd.SetOut(&out)
	apiCmd.SetErr(&errOut)
	rootCmd.SetArgs([]string{"api", "--path", "/api/v1/automations", "--paginate", "--list-key", "list"})

	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext: %v; stderr=%q", err, errOut.String())
	}
	rows := decodeNDJSONRows(t, out.String())
	if len(rows) != 1 || rows[0]["id"] != "a" {
		t.Fatalf("rows = %v, want only page 1's a", rows)
	}
}
