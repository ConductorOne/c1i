// Package transport is the auth-independent HTTP layer beneath c1i's
// authenticated API client: timeout, retry/backoff, user-agent, debug
// tracing, the empty-path-segment guard, and the redirect trust-scope guard.
// It knows nothing about OAuth, credentials, or the C1 API's own shape, so
// any package that speaks HTTP to a C1 host -- authenticated or not -- can
// depend on it without a cycle.
package transport

import "fmt"

// APIError is returned when a request completes with a non-2xx status. It
// carries the structured pieces so callers can branch on StatusCode and
// render machine-readable errors, while Error() preserves the original human
// string. Do() itself never returns this -- it hands back the response
// as-is for the caller to classify -- so callers construct it from a
// *Response when they choose to treat non-2xx as an error.
type APIError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API %s %s returned %d: %s", e.Method, e.Path, e.StatusCode, e.Body)
}

// PathError is returned when a request's path contains an empty segment -- a
// trailing slash or an interior "//" -- most commonly produced by an empty id
// argument (e.g. `c1i users get ""`), which client.Path renders as an empty
// segment. Left unchecked, the API 301-redirects a trailing-slash resource
// path to its parent collection, so the request would otherwise succeed and
// return the wrong data. Do() rejects it before any request is sent.
type PathError struct {
	Method string
	Path   string
}

func (e *PathError) Error() string {
	return fmt.Sprintf("refusing to send %s %s: empty path segment (an id argument was likely empty)", e.Method, e.Path)
}

// RedirectError is returned when a 3xx response's target changes the
// request's path, or keeps the path but points at a host outside the
// request host's trust scope (see redirectTripper and hostInScope); the host
// restriction keeps a same-path canonicalization hop from carrying the
// caller's credentials to an unrelated host.
type RedirectError struct {
	Method     string
	URL        string
	StatusCode int
	Location   string
}

func (e *RedirectError) Error() string {
	return fmt.Sprintf("refusing to follow redirect: %s %s returned %d to %q (a redirect that changes the resource path is never followed)", e.Method, e.URL, e.StatusCode, e.Location)
}

// RedirectLoopError is returned when a chain of allowed (same-path,
// in-scope-host) redirects doesn't settle within maxRedirectHops. Unlike
// RedirectError this is the server's own canonicalization not terminating,
// not a bad id argument.
type RedirectLoopError struct {
	Method string
	URL    string
	Hops   int
}

func (e *RedirectLoopError) Error() string {
	return fmt.Sprintf("refusing to follow redirect: %s %s did not settle after %d same-path redirects (a canonicalization loop on the server side)", e.Method, e.URL, e.Hops)
}
