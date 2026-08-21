package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/ConductorOne/c1i/internal/config"
	"github.com/ConductorOne/c1i/internal/keychain"
	"github.com/ConductorOne/c1i/internal/tokensource"
	"github.com/ConductorOne/c1i/internal/transport"
	"golang.org/x/oauth2"
)

// DefaultMaxRetries is how many times a retryable request is re-attempted
// (in addition to the initial try) when no override is supplied.
const DefaultMaxRetries = transport.DefaultMaxRetries

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

// isTokenError reports whether err is (or wraps) a rejected client_credentials
// grant. It powers two things: telling transport.Do to fail fast on it rather
// than burn the retry budget on credentials that won't fix themselves, and
// classifying it as an *AuthError once Do returns.
func isTokenError(err error) bool {
	var tokErr *tokensource.TokenError
	return errors.As(err, &tokErr)
}

// Token mints a fresh OAuth2 bearer token from the stored credentials for
// baseURL. It powers `c1i auth token`, giving agents a short-lived bearer to
// drive raw API calls without re-implementing the client_credentials exchange,
// and backs the MCP gateway's own bearer (the gateway accepts the same API
// token).
//
// ctx cancels the call (Ctrl-C / timeout) — see mintWithContext.
func Token(ctx context.Context, baseURL string, opts ...Option) (*oauth2.Token, error) {
	clientID, clientSecret, err := loadCredentials(baseURL)
	if err != nil {
		return nil, err
	}
	tokenSource, err := tokensource.NewTokenSource(ctx, clientID, clientSecret, baseURL, transportOpts(opts)...)
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

type Client struct {
	t       *transport.Client
	baseURL string
}

// buildConfig accumulates what Option mutates before a Client is built.
type buildConfig struct {
	maxRetries int
	debug      bool
}

// Option configures a Client (or a Token call) at construction time.
type Option func(*buildConfig)

// WithMaxRetries sets how many times a retryable request (429 or 5xx) is
// re-attempted, in addition to the first try. A negative value is treated as 0
// (no retries). The default is DefaultMaxRetries.
func WithMaxRetries(n int) Option {
	if n < 0 {
		n = 0
	}
	return func(c *buildConfig) { c.maxRetries = n }
}

// WithDebug enables HTTP wire tracing to stderr: each request's method and URL,
// then the response status and elapsed time (including every retry attempt).
// Headers and bodies are never logged, so credentials don't leak.
func WithDebug(enabled bool) Option {
	return func(c *buildConfig) { c.debug = enabled }
}

// transportOpts translates this package's Option (which only cmd/client.go
// and tests construct) into the transport.Option a lower-level package
// (tokensource) accepts, so both the REST client and the token mint it
// depends on share one set of retry/debug settings.
func transportOpts(opts []Option) []transport.Option {
	cfg := resolve(opts)
	return []transport.Option{
		transport.WithMaxRetries(cfg.maxRetries),
		transport.WithDebug(cfg.debug),
	}
}

func resolve(opts []Option) buildConfig {
	cfg := buildConfig{maxRetries: transport.DefaultMaxRetries}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// New authenticates to baseURL with the stored client_id/client_secret and
// returns a Client that sends every request through the shared transport
// layer (retry/backoff, timeout, user-agent, debug tracing, the path guard,
// and the redirect trust-scope guard) wrapped around an OAuth2
// client_credentials transport.
func New(ctx context.Context, baseURL string, opts ...Option) (*Client, error) {
	clientID, clientSecret, err := loadCredentials(baseURL)
	if err != nil {
		return nil, err
	}

	tokenSource, err := tokensource.NewTokenSource(ctx, clientID, clientSecret, baseURL, transportOpts(opts)...)
	if err != nil {
		return nil, &AuthError{fmt.Errorf("creating token source: %w", err)}
	}

	oauthClient := oauth2.NewClient(ctx, tokenSource)
	cfg := resolve(opts)
	t := transport.New(oauthClient.Transport,
		transport.WithMaxRetries(cfg.maxRetries),
		transport.WithDebug(cfg.debug),
		transport.WithNonRetryable(isTokenError),
	)
	return &Client{t: t, baseURL: baseURL}, nil
}

// NewForTesting returns a *Client that sends every request through hc's
// transport to baseURL, bypassing loadCredentials and the OAuth mint New
// performs. It exists so a test in another package (e.g. cmd's) can drive a
// command built on the shared client against a real httptest.Server without
// needing valid stored credentials or a live token endpoint. Accepts the same
// Options New does (e.g. WithMaxRetries(0), to keep a test that deliberately
// triggers 5xx/network retries from paying the real exponential backoff). No
// production code path calls this; c1i always authenticates through New.
//
// Only hc.Transport is used (falling back to http.DefaultTransport if nil);
// hc's own Timeout/CheckRedirect/Jar are not, since the returned Client
// applies its own timeout and redirect handling. No caller sets those on the
// http.Client it passes here today.
func NewForTesting(baseURL string, hc *http.Client, opts ...Option) *Client {
	var base http.RoundTripper
	if hc != nil {
		base = hc.Transport
	}
	cfg := resolve(opts)
	t := transport.New(base, transport.WithMaxRetries(cfg.maxRetries), transport.WithDebug(cfg.debug))
	return &Client{t: t, baseURL: baseURL}
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

// do sends req through the shared transport and applies this client's
// response policy: a non-2xx status is an *APIError, a rejected
// client_credentials grant is an *AuthError, and everything else (a path/
// redirect refusal, an exhausted transport-error retry budget) passes
// through as transport already classified it. It's the single chokepoint
// every request (Get/Post/Put/Patch/Delete/Request, including the `api`
// escape hatch) funnels through, so this policy lives here once rather than
// at each of the ~50 call sites that build a path.
func (c *Client) do(req *http.Request) ([]byte, error) {
	resp, err := c.t.Do(req)
	if err != nil {
		if isTokenError(err) {
			return nil, &AuthError{fmt.Errorf("token request failed: %w", err)}
		}
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{
			Method:     req.Method,
			Path:       req.URL.Path,
			StatusCode: resp.StatusCode,
			Body:       string(resp.Body),
		}
	}
	return resp.Body, nil
}
