package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ConductorOne/c1i/internal/tokensource"
)

// deterministicBackoff makes sleeps instant and jitter a no-op for the duration
// of a test, and returns a pointer to a slice recording each slept duration.
func deterministicBackoff(t *testing.T) *[]time.Duration {
	t.Helper()
	origSleep, origJitter := sleepFn, jitterFn
	var slept []time.Duration
	sleepFn = func(ctx context.Context, d time.Duration) error {
		slept = append(slept, d)
		return ctx.Err()
	}
	jitterFn = func(d time.Duration) time.Duration { return d }
	t.Cleanup(func() { sleepFn, jitterFn = origSleep, origJitter })
	return &slept
}

func newTestClient(srv *httptest.Server, maxRetries int) *Client {
	return &Client{httpClient: srv.Client(), baseURL: srv.URL, maxRetries: maxRetries}
}

func TestDoWithRetry_RetriesThenSucceeds(t *testing.T) {
	deterministicBackoff(t)
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	body, err := newTestClient(srv, 3).Get(context.Background(), "/x", nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3 (2 retries then success)", calls)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("body = %q", body)
	}
}

func TestDoWithRetry_ExhaustsRetriesAndReturnsError(t *testing.T) {
	slept := deterministicBackoff(t)
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := newTestClient(srv, 2).Get(context.Background(), "/x", nil)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if !strings.Contains(err.Error(), "returned 503") {
		t.Fatalf("error = %v, want it to mention 503", err)
	}
	if calls != 3 { // initial + 2 retries
		t.Fatalf("calls = %d, want 3", calls)
	}
	if len(*slept) != 2 {
		t.Fatalf("slept %d times, want 2", len(*slept))
	}
}

func TestDoWithRetry_NonRetryableStatusIsImmediate(t *testing.T) {
	deterministicBackoff(t)
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := newTestClient(srv, 5).Get(context.Background(), "/x", nil)
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (404 must not retry)", calls)
	}
}

func TestDoWithRetry_ZeroRetriesDisables(t *testing.T) {
	deterministicBackoff(t)
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := newTestClient(srv, 0).Get(context.Background(), "/x", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 with maxRetries=0", calls)
	}
}

func TestDoWithRetry_POSTBodyReplayedOnRetry(t *testing.T) {
	deterministicBackoff(t)
	var calls int
	var lastBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		b, _ := io.ReadAll(r.Body)
		lastBody = string(b)
		if calls < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv, 3).Post(context.Background(), "/x", map[string]any{"hello": "world"})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if !strings.Contains(lastBody, `"hello":"world"`) {
		t.Fatalf("retried request body = %q, want the original JSON replayed", lastBody)
	}
}

func TestDoWithRetry_HonorsRetryAfter(t *testing.T) {
	slept := deterministicBackoff(t)
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	if _, err := newTestClient(srv, 3).Get(context.Background(), "/x", nil); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(*slept) != 1 || (*slept)[0] != 2*time.Second {
		t.Fatalf("slept = %v, want a single 2s wait from Retry-After", *slept)
	}
}

func TestDoWithRetry_ContextCancellationStopsRetry(t *testing.T) {
	origSleep := sleepFn
	sleepFn = func(ctx context.Context, d time.Duration) error { return context.Canceled }
	t.Cleanup(func() { sleepFn = origSleep })

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := newTestClient(srv, 5).Get(context.Background(), "/x", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (cancel during first backoff)", calls)
	}
}

func TestIsIdempotent(t *testing.T) {
	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions, http.MethodTrace} {
		if !isIdempotent(m) {
			t.Errorf("isIdempotent(%s) = false, want true", m)
		}
	}
	for _, m := range []string{http.MethodPost, http.MethodPatch, http.MethodConnect} {
		if isIdempotent(m) {
			t.Errorf("isIdempotent(%s) = true, want false", m)
		}
	}
}

func TestIsRetryableStatus(t *testing.T) {
	// 429 is retryable for every method (rejected before processing).
	for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodPut} {
		if !isRetryableStatus(m, 429) {
			t.Errorf("isRetryableStatus(%s, 429) = false, want true", m)
		}
	}
	// 5xx is retryable only for idempotent methods.
	for _, c := range []int{500, 502, 503, 504} {
		if !isRetryableStatus(http.MethodGet, c) {
			t.Errorf("isRetryableStatus(GET, %d) = false, want true", c)
		}
		if isRetryableStatus(http.MethodPost, c) {
			t.Errorf("isRetryableStatus(POST, %d) = true, want false (non-idempotent)", c)
		}
	}
	// Never retryable, regardless of method.
	for _, c := range []int{200, 201, 400, 401, 403, 404, 409, 422, 501, 505} {
		if isRetryableStatus(http.MethodGet, c) {
			t.Errorf("isRetryableStatus(GET, %d) = true, want false", c)
		}
	}
}

func TestDoWithRetry_Post5xxNotRetried(t *testing.T) {
	deterministicBackoff(t)
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := newTestClient(srv, 5).Post(context.Background(), "/x", map[string]any{})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (POST must not retry on 5xx)", calls)
	}
}

// errDoer always fails at the transport level, like a dropped connection.
type errDoer struct{ calls int }

func (d *errDoer) Do(*http.Request) (*http.Response, error) {
	d.calls++
	return nil, errors.New("connection reset")
}

func TestDoWithRetry_TransportErrorRetriedOnlyForIdempotent(t *testing.T) {
	deterministicBackoff(t)

	getDoer := &errDoer{}
	getReq, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://x/y", nil)
	if _, err := doWithRetry(getDoer, getReq, 3); err == nil {
		t.Fatal("expected transport error to surface for GET")
	}
	if getDoer.calls != 4 { // 1 + 3 retries
		t.Fatalf("GET calls = %d, want 4", getDoer.calls)
	}

	postDoer := &errDoer{}
	postReq, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://x/y", strings.NewReader("{}"))
	if _, err := doWithRetry(postDoer, postReq, 3); err == nil {
		t.Fatal("expected transport error to surface for POST")
	}
	if postDoer.calls != 1 {
		t.Fatalf("POST calls = %d, want 1 (transport error must not retry POST)", postDoer.calls)
	}
}

func TestParseRetryAfter(t *testing.T) {
	if d, ok := parseRetryAfter("5"); !ok || d != 5*time.Second {
		t.Errorf("seconds: got (%v, %v)", d, ok)
	}
	if _, ok := parseRetryAfter(""); ok {
		t.Errorf("empty: want ok=false")
	}
	if _, ok := parseRetryAfter("not-a-number"); ok {
		t.Errorf("garbage: want ok=false")
	}
	if _, ok := parseRetryAfter("-3"); ok {
		t.Errorf("negative seconds: want ok=false")
	}
	if d, ok := parseRetryAfter("0"); !ok || d != 0 {
		t.Errorf("zero seconds: got (%v, %v), want (0, true)", d, ok)
	}
	// A huge value must be capped, not overflow into a negative duration.
	if d, ok := parseRetryAfter("99999999999"); !ok || d != retryMaxDelay {
		t.Errorf("overflow seconds: got (%v, %v), want (%v, true)", d, ok, retryMaxDelay)
	}
	// HTTP-date in the future yields a positive-ish duration.
	future := time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)
	if d, ok := parseRetryAfter(future); !ok || d <= 0 {
		t.Errorf("future date: got (%v, %v), want ok and positive", d, ok)
	}
	// HTTP-date in the past means retry now (ok, zero).
	past := time.Now().Add(-90 * time.Second).UTC().Format(http.TimeFormat)
	if d, ok := parseRetryAfter(past); !ok || d != 0 {
		t.Errorf("past date: got (%v, %v), want (0, true)", d, ok)
	}
}

func TestNextBackoff(t *testing.T) {
	origJitter := jitterFn
	jitterFn = func(d time.Duration) time.Duration { return d }
	t.Cleanup(func() { jitterFn = origJitter })

	// Exponential growth off retryBaseDelay when no Retry-After.
	if got := nextBackoff(0, nil); got != retryBaseDelay {
		t.Errorf("attempt 0 = %v, want %v", got, retryBaseDelay)
	}
	if got := nextBackoff(1, nil); got != retryBaseDelay*2 {
		t.Errorf("attempt 1 = %v, want %v", got, retryBaseDelay*2)
	}
	// Never exceeds the cap, even for a large attempt count.
	if got := nextBackoff(1000, nil); got != retryMaxDelay {
		t.Errorf("large attempt = %v, want cap %v", got, retryMaxDelay)
	}
	// Retry-After beyond the cap is clamped.
	resp := &http.Response{Header: http.Header{"Retry-After": []string{"3600"}}}
	if got := nextBackoff(0, resp); got != retryMaxDelay {
		t.Errorf("oversized Retry-After = %v, want cap %v", got, retryMaxDelay)
	}
	// Retry-After: 0 must floor to retryMinDelay, not collapse to a zero wait.
	zeroResp := &http.Response{Header: http.Header{"Retry-After": []string{"0"}}}
	if got := nextBackoff(0, zeroResp); got != retryMinDelay {
		t.Errorf("Retry-After 0 = %v, want floor %v", got, retryMinDelay)
	}
	// A valid mid-range Retry-After is honored as-is.
	midResp := &http.Response{Header: http.Header{"Retry-After": []string{"5"}}}
	if got := nextBackoff(0, midResp); got != 5*time.Second {
		t.Errorf("Retry-After 5 = %v, want 5s", got)
	}
}

func TestPath(t *testing.T) {
	tests := []struct {
		name   string
		format string
		ids    []string
		want   string
	}{
		{
			name:   "single segment",
			format: "/api/v1/functions/%s",
			ids:    []string{"abc123"},
			want:   "/api/v1/functions/abc123",
		},
		{
			name:   "multiple segments",
			format: "/api/v1/apps/%s/connectors/%s/mcp_tools/%s",
			ids:    []string{"app1", "conn2", "tool3"},
			want:   "/api/v1/apps/app1/connectors/conn2/mcp_tools/tool3",
		},
		{
			name:   "escapes reserved characters",
			format: "/api/v1/tasks/%s/action/deny",
			ids:    []string{"a?b#c d"},
			want:   "/api/v1/tasks/a%3Fb%23c%20d/action/deny",
		},
		{
			name:   "escapes slashes so a value cannot traverse segments",
			format: "/api/v1/functions/%s",
			ids:    []string{"a/b"},
			want:   "/api/v1/functions/a%2Fb",
		},
		{
			name:   "no ids returns format unchanged",
			format: "/api/v1/apps",
			ids:    nil,
			want:   "/api/v1/apps",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Path(tt.format, tt.ids...); got != tt.want {
				t.Errorf("Path(%q, %v) = %q, want %q", tt.format, tt.ids, got, tt.want)
			}
		})
	}
}

func TestPathLiteralPercent(t *testing.T) {
	// %% is a literal percent, not a verb, so it needs no id.
	if got := Path("/api/v1/x/%s/y%%z", "id"); got != "/api/v1/x/id/y%z" {
		t.Errorf("Path with literal %%%% = %q", got)
	}
}

func TestPathPanicsOnMismatch(t *testing.T) {
	cases := []struct {
		name   string
		format string
		ids    []string
	}{
		{"too few ids", "/api/v1/apps/%s/connectors/%s", []string{"a"}},
		{"too many ids", "/api/v1/apps/%s", []string{"a", "b"}},
		{"unsupported verb", "/api/v1/apps/%d", []string{"a"}},
		{"dangling percent", "/api/v1/apps/%", []string{"a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("Path(%q, %v) did not panic", tc.format, tc.ids)
				}
			}()
			_ = Path(tc.format, tc.ids...)
		})
	}
}

type errRoundTripper struct{ err error }

func (r errRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, r.err
}

func TestDoWrapsTokenErrorAsAuth(t *testing.T) {
	// A rejected client_credentials grant surfaces from httpClient.Do (wrapped
	// in *url.Error). do() should classify it as an AuthError.
	c := &Client{
		httpClient: &http.Client{Transport: errRoundTripper{&tokensource.TokenError{StatusCode: 401}}},
		baseURL:    "https://example.conductor.one",
	}
	_, err := c.Get(context.Background(), "/api/v1/x", nil)
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected AuthError for token rejection, got %T: %v", err, err)
	}
}

func TestDoNetworkErrorIsNotAuth(t *testing.T) {
	c := &Client{
		httpClient: &http.Client{Transport: errRoundTripper{errors.New("dial tcp: connection refused")}},
		baseURL:    "https://example.conductor.one",
	}
	_, err := c.Get(context.Background(), "/api/v1/x", nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	var authErr *AuthError
	if errors.As(err, &authErr) {
		t.Error("a plain network error must not be classified as AuthError")
	}
}

func TestDoReturnsAPIErrorWithStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"nope"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	c := &Client{httpClient: srv.Client(), baseURL: srv.URL}
	_, err := c.Get(context.Background(), "/api/v1/x", nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 404 || apiErr.Method != http.MethodGet {
		t.Errorf("APIError = %+v, want status 404 / GET", apiErr)
	}
}

func TestRequestSendsMethodHeaderBody(t *testing.T) {
	var gotMethod, gotHeader, gotCT, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotHeader = r.Header.Get("X-Custom")
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	out, err := newTestClient(srv, 0).Request(context.Background(), http.MethodPatch, "/x",
		[]byte(`{"a":1}`), map[string]string{"X-Custom": "yes"})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", gotMethod)
	}
	if gotHeader != "yes" {
		t.Errorf("X-Custom = %q, want yes", gotHeader)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotCT)
	}
	if gotBody != `{"a":1}` {
		t.Errorf("body = %q, want {\"a\":1}", gotBody)
	}
	if string(out) != `{"ok":true}` {
		t.Errorf("out = %q", out)
	}
}

func TestRequestNoBodyOmitsContentType(t *testing.T) {
	var gotCT string
	var hadBody bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		hadBody = len(b) > 0
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	if _, err := newTestClient(srv, 0).Request(context.Background(), http.MethodGet, "/x", nil, nil); err != nil {
		t.Fatalf("Request: %v", err)
	}
	if gotCT != "" {
		t.Errorf("Content-Type = %q, want empty for bodyless request", gotCT)
	}
	if hadBody {
		t.Errorf("expected no request body")
	}
}

func TestPatchSendsBodyAndContentType(t *testing.T) {
	var gotMethod, gotCT, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	if _, err := newTestClient(srv, 0).Patch(context.Background(), "/x", map[string]any{"hello": "world"}); err != nil {
		t.Fatalf("Patch: %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", gotMethod)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotCT)
	}
	if gotBody != `{"hello":"world"}` {
		t.Errorf("body = %q", gotBody)
	}
}
