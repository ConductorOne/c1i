package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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
		_ = apiCmd.Flags().Set("paginate", "false")
		_ = apiCmd.Flags().Set("list-key", "")
		_ = apiCmd.Flags().Set("limit", "0")
		// StringArray flags append on Set once pflag's internal "changed" bit
		// is true (which it is, forever, once any test has passed --query or
		// --header), so plain Set("query", "") wouldn't clear prior values —
		// it would append an empty string. Replace on the SliceValue
		// interface actually empties the backing slice.
		for _, name := range []string{"query", "header"} {
			if f := apiCmd.Flags().Lookup(name); f != nil {
				if sv, ok := f.Value.(pflag.SliceValue); ok {
					_ = sv.Replace([]string{})
				}
			}
		}
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
