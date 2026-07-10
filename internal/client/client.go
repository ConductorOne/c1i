package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"runtime/debug"
	"strconv"
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

type Client struct {
	httpClient *http.Client
	baseURL    string
	maxRetries int
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

func New(ctx context.Context, baseURL string, opts ...Option) (*Client, error) {
	service := config.KeychainService(baseURL)
	clientID, clientSecret, _, err := keychain.Load(service)
	if err != nil {
		// Try legacy keychain key for *.conductor.one domains.
		legacyService := config.LegacyKeychainService(baseURL)
		if legacyService != "" && legacyService != service {
			clientID, clientSecret, _, err = keychain.Load(legacyService)
			if err == nil {
				// Migrate: store under new key and delete old.
				_, _ = keychain.Store(service, clientID, clientSecret)
				_, _ = keychain.Delete(legacyService)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("loading credentials: %w", err)
		}
	}

	tokenSource, err := tokensource.NewTokenSource(ctx, clientID, clientSecret, baseURL)
	if err != nil {
		return nil, fmt.Errorf("creating token source: %w", err)
	}

	oauthClient := oauth2.NewClient(ctx, tokenSource)
	oauthClient.Transport = &userAgentTripper{next: oauthClient.Transport}

	c := &Client{
		httpClient: oauthClient,
		baseURL:    baseURL,
		maxRetries: DefaultMaxRetries,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
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

func (c *Client) do(req *http.Request) ([]byte, error) {
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
			return nil, fmt.Errorf("API %s %s returned %d: %s", req.Method, req.URL.Path, resp.StatusCode, string(body))
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
