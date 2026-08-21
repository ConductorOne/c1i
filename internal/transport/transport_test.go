package transport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
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
	return New(srv.Client().Transport, WithMaxRetries(maxRetries))
}

// get sends a GET to srv.URL+path through c and returns the body on success.
// Unlike the higher-level client.Client.Get, a non-2xx status is not itself
// an error here — see Response's doc comment — so callers that want to
// assert on status classify resp.StatusCode themselves.
func get(t *testing.T, c *Client, url string) (*Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c.Do(req)
}

func post(t *testing.T, c *Client, url, body string) (*Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return c.Do(req)
}

func TestDo_RetriesThenSucceeds(t *testing.T) {
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

	resp, err := get(t, newTestClient(srv, 3), srv.URL+"/x")
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3 (2 retries then success)", calls)
	}
	if string(resp.Body) != `{"ok":true}` {
		t.Fatalf("body = %q", resp.Body)
	}
}

// TestDo_ExhaustsRetriesAndReturnsResponse proves that once the retry budget
// is exhausted, the final (still non-2xx) response comes back as a Response,
// not an error — whether that counts as a failure is caller policy (see
// Response's doc comment), not Do's.
func TestDo_ExhaustsRetriesAndReturnsResponse(t *testing.T) {
	slept := deterministicBackoff(t)
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	resp, err := get(t, newTestClient(srv, 2), srv.URL+"/x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("StatusCode = %d, want 503", resp.StatusCode)
	}
	if calls != 3 { // initial + 2 retries
		t.Fatalf("calls = %d, want 3", calls)
	}
	if len(*slept) != 2 {
		t.Fatalf("slept %d times, want 2", len(*slept))
	}
}

func TestDo_NonRetryableStatusIsImmediate(t *testing.T) {
	deterministicBackoff(t)
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	resp, err := get(t, newTestClient(srv, 5), srv.URL+"/x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("StatusCode = %d, want 404", resp.StatusCode)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (404 must not retry)", calls)
	}
}

func TestDo_ZeroRetriesDisables(t *testing.T) {
	deterministicBackoff(t)
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	resp, err := get(t, newTestClient(srv, 0), srv.URL+"/x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("StatusCode = %d, want 500", resp.StatusCode)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 with maxRetries=0", calls)
	}
}

func TestDo_POSTBodyReplayedOnRetry(t *testing.T) {
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

	resp, err := post(t, newTestClient(srv, 3), srv.URL+"/x", `{"hello":"world"}`)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want 200", resp.StatusCode)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if !strings.Contains(lastBody, `"hello":"world"`) {
		t.Fatalf("retried request body = %q, want the original JSON replayed", lastBody)
	}
}

func TestDo_HonorsRetryAfter(t *testing.T) {
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

	if _, err := get(t, newTestClient(srv, 3), srv.URL+"/x"); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if len(*slept) != 1 || (*slept)[0] != 2*time.Second {
		t.Fatalf("slept = %v, want a single 2s wait from Retry-After", *slept)
	}
}

func TestDo_ContextCancellationStopsRetry(t *testing.T) {
	origSleep := sleepFn
	sleepFn = func(ctx context.Context, d time.Duration) error { return context.Canceled }
	t.Cleanup(func() { sleepFn = origSleep })

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := get(t, newTestClient(srv, 5), srv.URL+"/x")
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

func TestDo_Post5xxNotRetried(t *testing.T) {
	deterministicBackoff(t)
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	resp, err := post(t, newTestClient(srv, 5), srv.URL+"/x", "{}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("StatusCode = %d, want 503", resp.StatusCode)
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

func TestDo_TransportErrorRetriedOnlyForIdempotent(t *testing.T) {
	deterministicBackoff(t)

	getDoer := &errDoer{}
	getReq, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://x/y", nil)
	getClient := &Client{httpClient: &http.Client{Transport: doerTransport{getDoer}}, maxRetries: 3}
	if _, err := getClient.sendWithRetry(getReq); err == nil {
		t.Fatal("expected transport error to surface for GET")
	}
	if getDoer.calls != 4 { // 1 + 3 retries
		t.Fatalf("GET calls = %d, want 4", getDoer.calls)
	}

	postDoer := &errDoer{}
	postReq, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://x/y", strings.NewReader("{}"))
	postClient := &Client{httpClient: &http.Client{Transport: doerTransport{postDoer}}, maxRetries: 3}
	if _, err := postClient.sendWithRetry(postReq); err == nil {
		t.Fatal("expected transport error to surface for POST")
	}
	if postDoer.calls != 1 {
		t.Fatalf("POST calls = %d, want 1 (transport error must not retry POST)", postDoer.calls)
	}
}

// doerTransport adapts the errDoer's simpler Do-shaped interface to
// http.RoundTripper so it can back a *http.Client the same way the old
// package-level doWithRetry(doer, req, n) helper let a test drive the retry
// loop directly, without a real listener.
type doerTransport struct{ d *errDoer }

func (t doerTransport) RoundTrip(req *http.Request) (*http.Response, error) { return t.d.Do(req) }

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

// TestDo_NonRetryableHookShortCircuits proves WithNonRetryable stops the
// retry loop immediately, even for an idempotent method with retry budget
// left — the internal/client use case (a rejected client_credentials grant)
// must fail fast rather than retry a credential problem that won't fix
// itself.
func TestDo_NonRetryableHookShortCircuits(t *testing.T) {
	deterministicBackoff(t)
	doer := &errDoer{}
	c := New(doerTransport{doer}, WithMaxRetries(5), WithNonRetryable(func(error) bool { return true }))
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://x/y", nil)
	if _, err := c.Do(req); err == nil {
		t.Fatal("expected the transport error to surface")
	}
	if doer.calls != 1 {
		t.Fatalf("calls = %d, want 1 (non-retryable must not retry even a GET with budget left)", doer.calls)
	}
}

// TestNew_SendsUserAgent proves every request built by New's Client carries
// the fixed user-agent, regardless of which package constructs the Client.
func TestNew_SendsUserAgent(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(srv.Client().Transport)
	if _, err := get(t, c, srv.URL+"/x"); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if !strings.HasPrefix(got, "c1.ai/c1i") {
		t.Errorf("User-Agent = %q, want it to start with c1.ai/c1i", got)
	}
}

// TestNew_DebugWithFalseTracesNothingToStderr proves the default (no
// WithDebug) leaves stderr untouched -- the negative case for the test below,
// pinning that tracing is opt-in.
func TestNew_DebugFalseTracesNothingToStderr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	trace := captureStderr(t, func() {
		c := New(srv.Client().Transport)
		if _, err := get(t, c, srv.URL+"/x"); err != nil {
			t.Fatalf("Do: %v", err)
		}
	})
	if trace != "" {
		t.Errorf("stderr = %q, want no trace output without WithDebug", trace)
	}
}

// TestNew_DebugTracesToStderr proves New's own wiring — not a hand-built
// tripper chain, as TestClient_DebugTracesBothHops in redirect_narrow_test.go
// exercises — actually reaches stderr when WithDebug(true) is passed. This is
// the exact construction path every caller of New goes through (client,
// login, mcpgateway), so it's the seam that would silently reintroduce "the
// gateway path traces nothing" if New ever stopped wiring debug through.
func TestNew_DebugTracesToStderr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	trace := captureStderr(t, func() {
		c := New(srv.Client().Transport, WithDebug(true))
		if _, err := get(t, c, srv.URL+"/x"); err != nil {
			t.Fatalf("Do: %v", err)
		}
	})
	if !strings.Contains(trace, "> GET "+srv.URL+"/x") {
		t.Errorf("stderr trace = %q, want a request line for the GET", trace)
	}
	if !strings.Contains(trace, "< GET /x 200 OK") {
		t.Errorf("stderr trace = %q, want a 200 response line", trace)
	}
}

// captureStderr redirects os.Stderr for the duration of fn and returns
// everything written to it. Tests in this package never run with
// t.Parallel(), so serializing on the process-global os.Stderr is safe.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = orig })

	fn()

	_ = w.Close()
	os.Stderr = orig
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// TestNew_WiresTimeout pins that New actually sets the constructed Client's
// underlying http.Client.Timeout -- both the DefaultTimeout fallback and a
// WithTimeout override reach the wire, not just the buildConfig struct used
// to compute them. This is the highest-blast-radius declared behavior (every
// request this CLI sends now has a timeout that didn't exist before), so it
// gets its own pinning test rather than relying on the slower/hang-based
// tests elsewhere to catch a regression here indirectly.
func TestNew_WiresTimeout(t *testing.T) {
	c := New(http.DefaultTransport)
	if c.httpClient.Timeout != DefaultTimeout {
		t.Errorf("httpClient.Timeout = %v, want DefaultTimeout (%v)", c.httpClient.Timeout, DefaultTimeout)
	}

	const want = 42 * time.Second
	c = New(http.DefaultTransport, WithTimeout(want))
	if c.httpClient.Timeout != want {
		t.Errorf("httpClient.Timeout = %v, want the WithTimeout override (%v)", c.httpClient.Timeout, want)
	}
}
