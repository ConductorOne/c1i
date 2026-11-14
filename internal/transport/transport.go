package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptrace"
	"runtime/debug"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ConductorOne/c1i/internal/redact"
)

const (
	// DefaultMaxRetries is how many times a retryable request is re-attempted
	// (in addition to the initial try) when no override is supplied.
	DefaultMaxRetries = 4
	// DefaultTimeout bounds a single attempt (a retry gets a fresh budget,
	// not a shrinking share of one deadline). It's generous relative to any
	// endpoint this CLI calls today -- including an observed 182s MCP
	// gateway tools/call -- so it only ever fires for a truly hung
	// connection, not a slow-but-live one.
	DefaultTimeout = 10 * time.Minute
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
		return d/2 + time.Duration(rand.Int63n(int64(d/2)+1)) // #nosec G404 -- retry backoff jitter, not security-sensitive
	}
)

// Version is the c1i release version embedded via VCS build info ("dev" when
// unavailable, e.g. a plain `go run`). It is the single source for every
// client-identity string this CLI sends: the user-agent below, the MCP
// clientInfo.version an `mcp gateway` handshake reports (internal/mcpgateway
// reads this directly), and cmd.Version (`c1i version`, aliased to this).
//
// Exported and mutable (not a function returning an unexported value)
// specifically so a test can override it to a value that's never "dev" --
// see internal/mcpgateway's TestInitializeSendsRealClientVersion, which
// depends on this to distinguish "reads Version" from "hardcodes dev"; go
// test's own build normally reports "(devel)" -> "dev" too, so without an
// injectable override that test would pass by coincidence even against a
// hardcoded literal. Nothing in production code writes to it.
var Version = func() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}()

var userAgent = "c1.ai/c1i (version=" + Version + ")"

type userAgentTripper struct {
	next http.RoundTripper
}

func (uat *userAgentTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", userAgent)
	return uat.next.RoundTrip(req)
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
	_, _ = fmt.Fprintf(dt.out, "> %s %s\n", req.Method, redact.URL(req.URL.String()))
	resp, err := dt.next.RoundTrip(req)
	elapsed := time.Since(start).Round(time.Millisecond)
	if err != nil {
		_, _ = fmt.Fprintf(dt.out, "< %s %s error after %s: %v\n", req.Method, req.URL.Path, elapsed, err)
		return resp, err
	}
	_, _ = fmt.Fprintf(dt.out, "< %s %s %s (%s)\n", req.Method, req.URL.Path, resp.Status, elapsed)
	return resp, err
}

// Response is the outcome of one completed HTTP exchange, whatever its
// status code. Do returns one for any response that came back over the
// wire -- 200 or 404 or 500 alike -- and reserves the error return for a
// failure to complete the exchange at all (guard refusal, redirect
// refusal/loop, or a transport-level error that exhausted its retries).
// Deciding whether a given status counts as success is caller policy, not
// transport's: PollForToken's device-flow polling and this CLI's REST client
// disagree about which non-2xx statuses mean "fail now" vs "try again
// later", so Do can't make that call for both.
type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	// PriorSent reports that an earlier, retried attempt reached the server,
	// so a failure status here does not prove nothing was applied.
	PriorSent bool
}

// Client sends HTTP requests with a fixed, unforgettable set of protections:
// a redirect trust-scope guard, a per-attempt timeout, retry/backoff on
// transient failures, a user-agent, an empty-path-segment guard, and
// optional wire tracing. There is no way to obtain the underlying
// *http.Client -- Do is the only way to send a request through a Client -- so
// a caller cannot bypass any of this by reaching past it.
type Client struct {
	httpClient   *http.Client
	maxRetries   int
	nonRetryable func(error) bool
	maxRespBytes int64
	readOnly     bool
}

type buildConfig struct {
	maxRetries   int
	debug        io.Writer
	timeout      time.Duration
	nonRetryable func(error) bool
	maxRespBytes int64
	readOnly     bool
}

// Option configures a Client at construction time.
type Option func(*buildConfig)

// WithMaxRetries sets how many times a retryable request (429, or 5xx/
// transport-error for an idempotent method) is re-attempted, in addition to
// the first try. A negative value is treated as 0 (no retries). The default
// is DefaultMaxRetries.
func WithMaxRetries(n int) Option {
	if n < 0 {
		n = 0
	}
	return func(c *buildConfig) { c.maxRetries = n }
}

// WithDebug traces each attempt to w: the request's method and URL, then the
// response status and elapsed time. A nil w disables tracing. Headers and
// bodies are never logged, so credentials don't leak.
func WithDebug(w io.Writer) Option {
	return func(c *buildConfig) { c.debug = w }
}

// WithReadOnly declares every request through the Client read-only, so a POST
// that only carries a query (a docs search) is retried like a GET. Leave it off
// for any client that can send a mutation.
func WithReadOnly() Option {
	return func(c *buildConfig) { c.readOnly = true }
}

// WithTimeout overrides DefaultTimeout for a single attempt.
func WithTimeout(d time.Duration) Option {
	return func(c *buildConfig) { c.timeout = d }
}

// WithMaxResponseBytes caps how many bytes Do reads from a response body. A
// body larger than n makes Do fail rather than buffer it, so a hostile or
// misconfigured endpoint can't OOM the process before the caller inspects the
// content. n <= 0 (the default when unset) leaves the read unbounded, so
// existing callers are unchanged.
func WithMaxResponseBytes(n int64) Option {
	if n < 0 {
		n = 0
	}
	return func(c *buildConfig) { c.maxRespBytes = n }
}

// WithNonRetryable marks a transport-level error (one Do would otherwise
// retry for an idempotent method) as fatal instead, regardless of method or
// remaining budget. It exists for a caller layered on top of Client that
// mints its own credentials out-of-band (see internal/client's use for a
// rejected client_credentials grant): retrying a request that can never
// succeed until the credentials themselves change just delays reporting a
// fixable problem.
func WithNonRetryable(fn func(error) bool) Option {
	return func(c *buildConfig) { c.nonRetryable = fn }
}

// New returns a Client that sends requests through base (the innermost
// transport -- http.DefaultTransport for a plain caller, or an
// oauth2.Transport / other credential-attaching RoundTripper for an
// authenticated one). The redirect guard, user-agent, and (if enabled) debug
// tracing are layered around base in that order from the outside in, mirroring
// how an authenticated caller's credential-attaching transport sits: the
// redirect guard must be outermost so it can refuse before a credentialed
// request ever reaches an untrusted host, and debug tracing sits just inside
// it so --debug logs each hop of a followed redirect individually rather than
// one aggregate line for the whole chain.
func New(base http.RoundTripper, opts ...Option) *Client {
	cfg := buildConfig{maxRetries: DefaultMaxRetries, timeout: DefaultTimeout}
	for _, opt := range opts {
		opt(&cfg)
	}
	if base == nil {
		base = http.DefaultTransport
	}

	inner := http.RoundTripper(&userAgentTripper{next: base})
	if cfg.debug != nil {
		inner = &debugTripper{next: inner, out: cfg.debug}
	}
	tripper := http.RoundTripper(&redirectTripper{next: inner})

	return &Client{
		httpClient:   &http.Client{Transport: tripper, Timeout: cfg.timeout},
		maxRetries:   cfg.maxRetries,
		nonRetryable: cfg.nonRetryable,
		maxRespBytes: cfg.maxRespBytes,
		readOnly:     cfg.readOnly,
	}
}

// Do sends req, retrying a transient failure with exponential backoff and
// jitter, and returns the resulting Response for any status that comes back
// -- the caller decides what counts as success. err is non-nil only when no
// usable Response could be produced at all: an empty or dot path segment, a refused
// or non-terminating redirect, or a transport-level failure that exhausted
// its retry budget (or was marked non-retryable via WithNonRetryable).
//
// What's retried depends on the request (see isRetryableStatus / retrySafe):
// 429 is retried for any method, since the request was rejected before
// processing; 5xx and transport errors are retried only for idempotent
// methods and read-only search/list POSTs, so a POST/PATCH that may have
// committed server-side before the failure is not silently re-applied. The
// request body is replayed from req.GetBody on each attempt
// (http.NewRequestWithContext sets GetBody for the bytes.Reader/strings.Reader
// bodies this CLI uses).
func (c *Client) Do(req *http.Request) (*Response, error) {
	if err := CheckPath(req.Method, req.URL.EscapedPath()); err != nil {
		return nil, err
	}
	return c.sendWithRetry(req)
}

// CheckPath is the empty/dot-segment guard Do applies, exported so a caller
// can refuse a request it will never send (a --dry-run preview) identically.
func CheckPath(method, escapedPath string) error {
	if pathHasBadSegment(escapedPath) {
		return &PathError{Method: method, Path: escapedPath}
	}
	return nil
}

// AttemptError wraps every error Do returns once an attempt has started.
// Sent is set exactly when the final attempt's request headers were written
// (httptrace WroteHeaders); PriorSent when any earlier attempt's were. It is
// transparent: Error and Unwrap defer to Err.
type AttemptError struct {
	Err       error
	Sent      bool
	PriorSent bool
}

func (e *AttemptError) Error() string { return e.Err.Error() }
func (e *AttemptError) Unwrap() error { return e.Err }

func (c *Client) sendWithRetry(req *http.Request) (*Response, error) {
	ctx := req.Context()
	priorSent := false
	for attempt := 0; ; attempt++ {
		if attempt > 0 && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, &AttemptError{Err: fmt.Errorf("rewinding request body for retry: %w", err), PriorSent: priorSent}
			}
			req.Body = body
		}

		// WroteHeaders may fire on the transport's write goroutine.
		var sent atomic.Bool
		trace := &httptrace.ClientTrace{WroteHeaders: func() { sent.Store(true) }}
		attemptReq := req.WithContext(httptrace.WithClientTrace(ctx, trace))
		fail := func(err error) error {
			return &AttemptError{Err: err, Sent: sent.Load(), PriorSent: priorSent}
		}

		resp, err := c.httpClient.Do(attemptReq) // #nosec G704 -- req.URL is the operator's own --url/config target, not attacker-supplied input to an internal request
		if err != nil {
			redact.ErrorURL(err)
			if c.nonRetryable != nil && c.nonRetryable(err) {
				return nil, fail(err)
			}
			// A redirect refusal/loop (see redirectTripper) is permanent for a
			// given request shape -- retrying would just redirect the same way
			// again -- so surface it immediately instead of burning the retry
			// budget on it.
			var redirErr *RedirectError
			if errors.As(err, &redirErr) {
				return nil, fail(redirErr)
			}
			var redirLoopErr *RedirectLoopError
			if errors.As(err, &redirLoopErr) {
				return nil, fail(redirLoopErr)
			}
			// Transport-level failure (connection reset, timeout, ...). We
			// can't tell whether the server processed the request, so only
			// retry idempotent methods.
			if (c.readOnly || retrySafe(req.Method, req.URL.EscapedPath())) && attempt < c.maxRetries {
				priorSent = priorSent || sent.Load()
				if serr := sleepFn(ctx, nextBackoff(attempt, nil)); serr != nil {
					return nil, &AttemptError{Err: serr, PriorSent: priorSent}
				}
				continue
			}
			return nil, fail(err)
		}

		body, readErr := c.readBody(resp.Body)
		_ = resp.Body.Close()

		if statusRetryable(resp.StatusCode, c.readOnly || retrySafe(req.Method, req.URL.EscapedPath())) && attempt < c.maxRetries {
			priorSent = priorSent || sent.Load()
			if serr := sleepFn(ctx, nextBackoff(attempt, resp)); serr != nil {
				return nil, &AttemptError{Err: serr, PriorSent: priorSent}
			}
			continue
		}

		// A read failure on a non-2xx response still yields a Response (with
		// whatever body was read) rather than an error: the status itself is
		// the meaningful outcome, and the caller's classification of it
		// shouldn't be masked by an incidental body-read problem. On a 2xx,
		// though, the body IS the result, so a read failure there must
		// surface as an error.
		if resp.StatusCode >= 200 && resp.StatusCode < 300 && readErr != nil {
			return nil, fail(readErr)
		}
		return &Response{StatusCode: resp.StatusCode, Header: resp.Header, Body: body, PriorSent: priorSent}, nil
	}
}

// readBody reads a response body, refusing one over maxRespBytes when set.
func (c *Client) readBody(r io.Reader) ([]byte, error) {
	if c.maxRespBytes <= 0 {
		return io.ReadAll(r)
	}
	body, err := io.ReadAll(io.LimitReader(r, c.maxRespBytes+1))
	if err != nil {
		return body, err
	}
	if int64(len(body)) > c.maxRespBytes {
		return nil, fmt.Errorf("response body exceeds %d bytes", c.maxRespBytes)
	}
	return body, nil
}

// isIdempotent reports whether method is idempotent per RFC 9110. POST and
// PATCH are not; see retrySafe for the read-only POST exception.
func isIdempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut,
		http.MethodDelete, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

// retrySafe reports whether re-sending a request after an ambiguous failure is
// safe: an idempotent method, or a POST to a read-only search/list route. Every
// such route in the C1 API spec (/api/v1/search/*, .../search, .../list) is a
// Search/List/Count/Get RPC; POST there only carries the filter body.
func retrySafe(method, path string) bool {
	if isIdempotent(method) {
		return true
	}
	if method != http.MethodPost || !strings.HasPrefix(path, "/api/v1/") {
		return false
	}
	last := path[strings.LastIndex(path, "/")+1:]
	return strings.HasPrefix(path, "/api/v1/search/") || last == "search" || last == "list"
}

// isRetryableStatus reports whether a response status warrants a retry for the
// given request. 429 (rate limited) is always safe to retry: the request was
// rejected before processing. The transient 5xx codes are retried only when
// retrySafe. 501/505 and 4xx (other than 429) are never retried.
func isRetryableStatus(method, path string, code int) bool {
	return statusRetryable(code, retrySafe(method, path))
}

// statusRetryable is isRetryableStatus once retry-safety is already decided.
func statusRetryable(code int, safe bool) bool {
	switch code {
	case http.StatusTooManyRequests: // 429
		return true
	case http.StatusInternalServerError, // 500
		http.StatusBadGateway,         // 502
		http.StatusServiceUnavailable, // 503
		http.StatusGatewayTimeout:     // 504
		return safe
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
