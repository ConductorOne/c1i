package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/ConductorOne/c1i/internal/config"
	"github.com/ConductorOne/c1i/internal/keychain"
	"github.com/ConductorOne/c1i/internal/tokensource"
	"golang.org/x/oauth2"
)

const (
	// DefaultMaxRetries is how many times a retryable request is re-attempted
	// (in addition to the initial try) when no override is supplied.
	DefaultMaxRetries = 4
	// retryBaseDelay is the first backoff interval; it doubles each attempt.
	retryBaseDelay = 500 * time.Millisecond
	// retryMinDelay floors every wait so a zero/past Retry-After can't collapse
	// into a back-to-back retry burst.
	retryMinDelay = 100 * time.Millisecond
	// retryMaxDelay caps a single backoff interval so exponential growth
	// (or a hostile Retry-After) can't stall the CLI indefinitely.
	retryMaxDelay = 30 * time.Second
	// maxRetryAfterSecs bounds a numeric Retry-After before the multiply below,
	// so a huge value can't overflow time.Duration (it's clamped to the cap
	// anyway).
	maxRetryAfterSecs = int(retryMaxDelay / time.Second)
)

// sleepFn and jitterFn are indirected so tests can make backoff deterministic
// and instant. Production uses a context-aware sleep and full jitter.
var (
	sleepFn = func(ctx context.Context, d time.Duration) error {
		if d <= 0 {
			return ctx.Err()
		}
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			return nil
		}
	}
	// jitterFn spreads retries across [d/2, d] ("full jitter", lower half
	// clamped) so many clients retrying together don't thundering-herd.
	jitterFn = func(d time.Duration) time.Duration {
		if d <= 0 {
			return 0
		}
		return d/2 + time.Duration(rand.Int63n(int64(d/2)+1))
	}
)

var userAgent = func() string {
	version := "dev"
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
	return "c1.ai/c1i (version=" + version + ")"
}()

type userAgentTripper struct {
	next http.RoundTripper
}

func (uat *userAgentTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", userAgent)
	return uat.next.RoundTrip(req)
}

// redirectStatuses is the set of 3xx codes net/http's own Client would
// otherwise follow (see net/http's internal redirectBehavior). 304 (Not
// Modified) and the unused 305/306 are deliberately excluded: they aren't
// Location-based redirects net/http ever follows.
var redirectStatuses = map[int]bool{
	http.StatusMovedPermanently:  true, // 301
	http.StatusFound:             true, // 302
	http.StatusSeeOther:          true, // 303
	http.StatusTemporaryRedirect: true, // 307
	http.StatusPermanentRedirect: true, // 308
}

// maxRedirectHops bounds how many same-path redirects redirectTripper will
// follow for one logical request. Real scheme/host canonicalization is one
// hop, occasionally two (e.g. an upgrade hop plus a regional-domain hop);
// this leaves headroom for that while still turning a misconfigured
// canonicalization loop into a bounded, clearly-reported failure rather than
// a hang.
const maxRedirectHops = 5

// redirectTripper follows a 3xx only when the target's escaped path matches
// the request's exactly AND the target host is in the same trust scope as
// the request host (see hostInScope); anything else — a changed path, or a
// same-path redirect to an unrelated host — is refused as *RedirectError.
// The host check exists because this tripper sits outside the oauth2
// transport: following to an arbitrary host would hand that host the
// caller's bearer token, which is exactly what net/http's own redirect
// handling strips Authorization to prevent. It intercepts at the
// RoundTripper layer rather than via http.Client.CheckRedirect because
// CheckRedirect's signature only exposes the *next* request, not the
// response that triggered it, so it can't report the status/Location a
// refusal needs to show.
type redirectTripper struct {
	next http.RoundTripper
}

func (rt *redirectTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for hop := 0; ; hop++ {
		resp, err := rt.next.RoundTrip(req)
		if err != nil || !redirectStatuses[resp.StatusCode] {
			return resp, err
		}
		loc := resp.Header.Get("Location")
		_ = resp.Body.Close()

		target, ok := resolveAllowedRedirect(req.URL, loc)
		if !ok {
			return nil, &RedirectError{
				Method:     req.Method,
				URL:        req.URL.String(),
				StatusCode: resp.StatusCode,
				Location:   loc,
			}
		}
		if hop == maxRedirectHops-1 {
			return nil, &RedirectLoopError{
				Method: req.Method,
				URL:    req.URL.String(),
				Hops:   hop + 1,
			}
		}
		next, cerr := redirectedRequest(req, target)
		if cerr != nil {
			return nil, cerr
		}
		req = next
	}
}

// resolveAllowedRedirect resolves location — a bare path, a path-relative
// reference, or an absolute URL — against base per RFC 3986 (the same
// resolution net/http's own redirect-following uses), and reports the
// resolved target only when both hold: its escaped path is identical to
// base's (EscapedPath, not the decoded Path, so a percent-encoded separator
// like %2F isn't silently decoded into one — matching pathHasEmptySegment
// and do()'s guard; a trailing-slash difference therefore counts as
// changed, since that's the exact shape the id-normalizes-to-collection
// bypass produces), and its host is in the same trust scope as base's (see
// hostInScope). An empty Location, or one url.Parse rejects, never resolves.
func resolveAllowedRedirect(base *url.URL, location string) (*url.URL, bool) {
	if location == "" {
		return nil, false
	}
	target, err := base.Parse(location)
	if err != nil {
		return nil, false
	}
	if target.EscapedPath() != base.EscapedPath() {
		return nil, false
	}
	if !hostInScope(base, target) {
		return nil, false
	}
	return target, true
}

// hostInScope reports whether target's host may be trusted with base's
// credentials: identical (ignoring scheme/port — this alone covers a bare host
// like "localhost"), or one is "<label>." prepended
// to the other with at least two labels in the target. The "." is inside the
// comparison to enforce a label boundary, so "eviltenant.example" is not a
// subdomain of "tenant.example"; the two-label floor rejects a bare apex that
// could not be a real canonicalization.
func hostInScope(base, target *url.URL) bool {
	a := strings.ToLower(base.Hostname())
	b := strings.ToLower(target.Hostname())
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	if !strings.Contains(b, ".") {
		return false
	}
	return strings.HasSuffix(a, "."+b) || strings.HasSuffix(b, "."+a)
}

// redirectedRequest builds the request for an allowed (same-path) redirect
// hop: same method and headers as req, pointed at target. The body is
// re-obtained from GetBody (set by http.NewRequestWithContext for the
// bytes/strings-backed bodies this package sends) since req's original Body
// reader was already consumed sending the hop that produced the redirect.
func redirectedRequest(req *http.Request, target *url.URL) (*http.Request, error) {
	next := req.Clone(req.Context())
	next.URL = target
	next.Host = ""
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, fmt.Errorf("rewinding request body for redirect: %w", err)
		}
		next.Body = body
	}
	return next, nil
}

type Client struct {
	httpClient *http.Client
	baseURL    string
	maxRetries int
	debug      bool
}

// Option configures a Client at construction time.
type Option func(*Client)

// WithMaxRetries sets how many times a retryable request (429 or 5xx) is
// re-attempted, in addition to the first try. A negative value is treated as 0
// (no retries). The default is DefaultMaxRetries.
func WithMaxRetries(n int) Option {
	if n < 0 {
		n = 0
	}
	return func(c *Client) { c.maxRetries = n }
}

// WithDebug enables HTTP wire tracing to stderr: each request's method and URL,
// then the response status and elapsed time (including every retry attempt).
// Headers and bodies are never logged, so credentials don't leak.
func WithDebug(enabled bool) Option {
	return func(c *Client) { c.debug = enabled }
}

// debugTripper logs one line before and after each attempt. It wraps the
// outermost transport so it measures the full round trip and sees retried
// attempts individually.
type debugTripper struct {
	next http.RoundTripper
	out  io.Writer
}

func (dt *debugTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	_, _ = fmt.Fprintf(dt.out, "> %s %s\n", req.Method, req.URL.String())
	resp, err := dt.next.RoundTrip(req)
	elapsed := time.Since(start).Round(time.Millisecond)
	if err != nil {
		_, _ = fmt.Fprintf(dt.out, "< %s %s error after %s: %v\n", req.Method, req.URL.Path, elapsed, err)
		return resp, err
	}
	_, _ = fmt.Fprintf(dt.out, "< %s %s %s (%s)\n", req.Method, req.URL.Path, resp.Status, elapsed)
	return resp, err
}

// loadCredentials resolves the stored client_id/client_secret for baseURL,
// falling back to (and migrating from) the legacy *.conductor.one keychain key.
// New and Token share it so token minting resolves credentials identically to
// an authenticated request.
func loadCredentials(baseURL string) (clientID, clientSecret string, err error) {
	service := config.KeychainService(baseURL)
	clientID, clientSecret, _, err = keychain.Load(service)
	if err != nil {
		// Try legacy keychain key for *.conductor.one domains.
		legacyService := config.LegacyKeychainService(baseURL)
		if legacyService != "" && legacyService != service {
			clientID, clientSecret, _, err = keychain.Load(legacyService)
			if err == nil {
				// Migrate: store under the new key, and only delete the legacy
				// copy once the new one is safely written. Deleting first (or
				// unconditionally) could drop the user's only credentials if
				// Store fails — keychain.Store can error and internally clears
				// the target before writing — forcing a re-login.
				if _, serr := keychain.Store(service, clientID, clientSecret); serr == nil {
					_, _ = keychain.Delete(legacyService)
				}
			}
		}
		if err != nil {
			return "", "", &AuthError{fmt.Errorf("loading credentials: %w", err)}
		}
	}
	return clientID, clientSecret, nil
}

// Token mints a fresh OAuth2 bearer token from the stored credentials for
// baseURL. It powers `c1i auth token`, giving agents a short-lived bearer to
// drive raw API calls without re-implementing the client_credentials exchange.
//
// ctx cancels the call (Ctrl-C / timeout) — see mintWithContext.
func Token(ctx context.Context, baseURL string) (*oauth2.Token, error) {
	clientID, clientSecret, err := loadCredentials(baseURL)
	if err != nil {
		return nil, err
	}
	tokenSource, err := tokensource.NewTokenSource(ctx, clientID, clientSecret, baseURL)
	if err != nil {
		return nil, &AuthError{fmt.Errorf("creating token source: %w", err)}
	}
	return mintWithContext(ctx, tokenSource)
}

// mintWithContext calls ts.Token() but honors ctx cancellation. The
// oauth2.TokenSource interface has no per-call context and tokensource.Token()
// mints on its own background request, so the mint runs on a goroutine and we
// return as soon as ctx is done. The channel is buffered so the goroutine can
// always finish and be collected even after we've returned; the tokensource
// HTTP client has its own timeout, so a hung token host can't keep that
// goroutine (or its socket) alive indefinitely. A mint error is wrapped as
// AuthError (exit 3); a ctx cancellation returns ctx.Err() unwrapped so callers
// can distinguish it from an authentication failure.
func mintWithContext(ctx context.Context, ts oauth2.TokenSource) (*oauth2.Token, error) {
	type result struct {
		tok *oauth2.Token
		err error
	}
	ch := make(chan result, 1)
	go func() {
		tok, err := ts.Token()
		ch <- result{tok, err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, &AuthError{fmt.Errorf("minting token: %w", r.err)}
		}
		return r.tok, nil
	}
}

func New(ctx context.Context, baseURL string, opts ...Option) (*Client, error) {
	clientID, clientSecret, err := loadCredentials(baseURL)
	if err != nil {
		return nil, err
	}

	tokenSource, err := tokensource.NewTokenSource(ctx, clientID, clientSecret, baseURL)
	if err != nil {
		return nil, &AuthError{fmt.Errorf("creating token source: %w", err)}
	}

	oauthClient := oauth2.NewClient(ctx, tokenSource)

	c := &Client{
		httpClient: oauthClient,
		baseURL:    baseURL,
		maxRetries: DefaultMaxRetries,
	}
	for _, opt := range opts {
		opt(c)
	}
	// redirectTripper must be outermost: when it follows an allowed
	// same-path redirect it calls inner.RoundTrip again for the second hop,
	// so debugTripper has to sit inside it (not outside, as a single wrap
	// around the whole chain would) for --debug to log both hops instead of
	// only the first request and the final response.
	inner := http.RoundTripper(&userAgentTripper{next: oauthClient.Transport})
	if c.debug {
		inner = &debugTripper{next: inner, out: os.Stderr}
	}
	oauthClient.Transport = &redirectTripper{next: inner}
	return c, nil
}

// NewForTesting returns a *Client that sends every request through hc to
// baseURL, bypassing loadCredentials and the OAuth mint New performs. It
// exists so a test in another package (e.g. cmd's) can drive a command built
// on the shared client against a real httptest.Server without needing valid
// stored credentials or a live token endpoint — mirroring what this
// package's own client_test.go does internally via newTestClient, just
// exported for cross-package use. Accepts the same Options New does (e.g.
// WithMaxRetries(0), to keep a test that deliberately triggers 5xx/network
// retries from paying New's real exponential backoff). No production code
// path calls this; c1i always authenticates through New.
func NewForTesting(baseURL string, hc *http.Client, opts ...Option) *Client {
	next := hc.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	hc.Transport = &redirectTripper{next: next}
	c := &Client{httpClient: hc, baseURL: baseURL, maxRetries: DefaultMaxRetries}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Path builds an API path from a printf-style format string, URL-escaping each
// argument as a single path segment. Use it whenever a user-supplied ID is
// interpolated into a request path so that values containing "?", "#", spaces,
// or other reserved characters address the intended resource instead of being
// truncated or mangled by url.Parse. Every format verb must be %s.
//
// format is always a compile-time constant in callers, so a verb/arg mismatch
// is a programming bug — Path panics rather than let fmt.Sprintf emit a
// corrupted path (%!s(MISSING) / %!(EXTRA ...)) that would be sent to the API.
func Path(format string, ids ...string) string {
	if n := countStringVerbs(format); n != len(ids) {
		panic(fmt.Sprintf("client.Path: format %q has %d %%s verb(s) but got %d id(s)", format, n, len(ids)))
	}
	escaped := make([]any, len(ids))
	for i, id := range ids {
		escaped[i] = url.PathEscape(id)
	}
	return fmt.Sprintf(format, escaped...)
}

// countStringVerbs returns the number of %s verbs in format. It panics if the
// format contains any other verb (e.g. %d) or a dangling %, since Path only
// supports %s and a stray verb would corrupt the path.
func countStringVerbs(format string) int {
	n := 0
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		if i+1 >= len(format) {
			panic(fmt.Sprintf("client.Path: dangling %% in format %q", format))
		}
		switch format[i+1] {
		case '%': // literal percent, not a verb
		case 's':
			n++
		default:
			panic(fmt.Sprintf("client.Path: unsupported verb %%%c in format %q (only %%s allowed)", format[i+1], format))
		}
		i++ // skip the character after %
	}
	return n
}

func (c *Client) Get(ctx context.Context, path string, queryParams map[string]string) ([]byte, error) {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, err
	}
	if len(queryParams) > 0 {
		q := u.Query()
		for k, v := range queryParams {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

func (c *Client) Post(ctx context.Context, path string, body any) ([]byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

func (c *Client) Put(ctx context.Context, path string, body any) ([]byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

func (c *Client) Delete(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

func (c *Client) Patch(ctx context.Context, path string, body any) ([]byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

// Request performs an arbitrary method against path with an optional raw JSON
// body and extra headers. It's the generic path behind the `api` escape hatch;
// typed commands use the Get/Post/Put/Delete/Patch helpers instead. A non-nil
// body sets Content-Type: application/json (overridable via headers), and the
// body is replayed on retries via the reader's GetBody.
func (c *Client) Request(ctx context.Context, method, path string, body []byte, headers map[string]string) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return c.do(req)
}

// do is the single chokepoint every request (Get/Post/Put/Patch/Delete/
// Request, including the `api` escape hatch) funnels through before it hits
// the wire — so the empty-path-segment guard lives here once, rather than at
// each of the ~50 call sites that build a path. EscapedPath is checked (not
// the decoded Path) so an id containing an escaped "/" (%2F) can't be
// mistaken for a path separator.
func (c *Client) do(req *http.Request) ([]byte, error) {
	if p := req.URL.EscapedPath(); pathHasEmptySegment(p) {
		return nil, &PathError{Method: req.Method, Path: p}
	}
	return doWithRetry(c.httpClient, req, c.maxRetries)
}

// httpDoer is the subset of *http.Client used by doWithRetry, so tests can
// drive the retry loop with a plain client pointed at an httptest server
// instead of the OAuth-wrapped one.
type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// doWithRetry sends req, retrying transient failures up to maxRetries additional
// times with exponential backoff + jitter, honoring a Retry-After header when
// present. What is retried depends on the method (see isRetryableStatus /
// isIdempotent): 429 is retried for any method, but 5xx and transport errors are
// retried only for idempotent methods, so a POST/PATCH that may have committed
// server-side before the failure is not silently re-applied.
//
// The request body is replayed from req.GetBody on each attempt
// (http.NewRequestWithContext sets GetBody for the bytes.Reader bodies this
// package uses), so a retried request re-sends the same payload.
func doWithRetry(doer httpDoer, req *http.Request, maxRetries int) ([]byte, error) {
	ctx := req.Context()
	for attempt := 0; ; attempt++ {
		if attempt > 0 && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, fmt.Errorf("rewinding request body for retry: %w", err)
			}
			req.Body = body
		}

		resp, err := doer.Do(req)
		if err != nil {
			// A rejected client_credentials grant (token minting is lazy, so it
			// runs on the first request) surfaces here. Classify it as an auth
			// failure and don't retry — the credentials won't fix themselves.
			var tokErr *tokensource.TokenError
			if errors.As(err, &tokErr) {
				return nil, &AuthError{fmt.Errorf("token request failed: %w", err)}
			}
			// A redirect (see redirectTripper) is permanent for a given
			// request shape — retrying would just redirect the same way
			// again — so surface it immediately instead of burning the
			// retry budget on it. Same for a chain that hit the hop bound:
			// it already tried following maxRedirectHops times internally,
			// invisible to this loop, and would do so identically again.
			var redirErr *RedirectError
			if errors.As(err, &redirErr) {
				return nil, redirErr
			}
			var redirLoopErr *RedirectLoopError
			if errors.As(err, &redirLoopErr) {
				return nil, redirLoopErr
			}
			// Transport-level failure (connection reset, timeout, ...). We
			// can't tell whether the server processed the request, so only
			// retry idempotent methods.
			if isIdempotent(req.Method) && attempt < maxRetries {
				if serr := sleepFn(ctx, nextBackoff(attempt, nil)); serr != nil {
					return nil, serr
				}
				continue
			}
			return nil, err
		}

		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			if isRetryableStatus(req.Method, resp.StatusCode) && attempt < maxRetries {
				if serr := sleepFn(ctx, nextBackoff(attempt, resp)); serr != nil {
					return nil, serr
				}
				continue
			}
			return nil, &APIError{
				Method:     req.Method,
				Path:       req.URL.Path,
				StatusCode: resp.StatusCode,
				Body:       string(body),
			}
		}

		if readErr != nil {
			return nil, readErr
		}
		return body, nil
	}
}

// isIdempotent reports whether re-sending method after an ambiguous failure is
// safe. Per RFC 9110 these methods are idempotent; POST and PATCH are not and
// so are never retried on 5xx / transport errors.
func isIdempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut,
		http.MethodDelete, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

// isRetryableStatus reports whether a response status warrants a retry for the
// given method. 429 (rate limited) is always safe to retry: the request was
// rejected before processing. The transient 5xx codes are retried only for
// idempotent methods. 501/505 and 4xx (other than 429) are never retried.
func isRetryableStatus(method string, code int) bool {
	switch code {
	case http.StatusTooManyRequests: // 429
		return true
	case http.StatusInternalServerError, // 500
		http.StatusBadGateway,         // 502
		http.StatusServiceUnavailable, // 503
		http.StatusGatewayTimeout:     // 504
		return isIdempotent(method)
	default:
		return false
	}
}

// nextBackoff returns how long to wait before the next attempt. A valid
// Retry-After header (from a 429/503) wins and is honored as-is; otherwise it's
// exponential backoff (retryBaseDelay * 2^attempt) spread with jitter. Either
// way the result is bounded to [retryMinDelay, retryMaxDelay] so a zero/hostile
// Retry-After can neither hammer the server nor stall the CLI.
func nextBackoff(attempt int, resp *http.Response) time.Duration {
	if resp != nil {
		if d, ok := parseRetryAfter(resp.Header.Get("Retry-After")); ok {
			return boundDelay(d)
		}
	}
	// Shift instead of math.Pow to stay in integer nanoseconds; guard the
	// exponent so a large attempt count can't overflow the shift.
	delay := retryMaxDelay
	if attempt < 32 {
		if d := retryBaseDelay << attempt; d > 0 && d < retryMaxDelay {
			delay = d
		}
	}
	return boundDelay(jitterFn(delay))
}

// boundDelay clamps d into [retryMinDelay, retryMaxDelay]. The floor guarantees
// a positive wait (so retries never busy-loop), and the cap keeps a single wait
// bounded.
func boundDelay(d time.Duration) time.Duration {
	if d < retryMinDelay {
		return retryMinDelay
	}
	if d > retryMaxDelay {
		return retryMaxDelay
	}
	return d
}

// parseRetryAfter interprets a Retry-After header value, which is either an
// integer number of seconds or an HTTP date. Returns (0, false) when absent or
// unparseable. A numeric value is capped at maxRetryAfterSecs before the
// multiply so it can't overflow time.Duration; callers bound the result anyway.
func parseRetryAfter(v string) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}
	// ParseInt (not Atoi) so a large value parses to 64-bit on 32-bit builds too,
	// and is clamped rather than falling through to the date branch.
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
		if secs < 0 {
			return 0, false
		}
		if secs > int64(maxRetryAfterSecs) {
			return retryMaxDelay, true
		}
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d, true
		}
		return 0, true // date in the past → retry after the floor delay
	}
	return 0, false
}
