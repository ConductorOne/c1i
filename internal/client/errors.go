package client

import "fmt"

// APIError is returned when the C1 API responds with a non-2xx status. It
// carries the structured pieces so callers can branch on StatusCode and render
// machine-readable errors, while Error() preserves the original human string.
type APIError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API %s %s returned %d: %s", e.Method, e.Path, e.StatusCode, e.Body)
}

// AuthError wraps failures to load credentials or mint a token — i.e. the
// caller is not authenticated, as opposed to an API request being rejected.
// Callers use errors.As to map it to an auth-specific exit code.
type AuthError struct{ Err error }

func (e *AuthError) Error() string { return e.Err.Error() }
func (e *AuthError) Unwrap() error { return e.Err }

// PathError is returned when a request's path contains an empty segment — a
// trailing slash or an interior "//" — most commonly produced by an empty id
// argument (e.g. `c1i users get ""`), which client.Path renders as an empty
// segment. Left unchecked, the API 301-redirects a trailing-slash resource
// path to its parent collection, so the request would otherwise succeed and
// return the wrong data. do() rejects it before any request is sent.
type PathError struct {
	Method string
	Path   string
}

func (e *PathError) Error() string {
	return fmt.Sprintf("refusing to send %s %s: empty path segment (an id argument was likely empty)", e.Method, e.Path)
}

// RedirectError is returned when a request receives a 3xx response. c1i's
// http.Client never follows a redirect (see redirectTripper in client.go)
// because this API performs zero redirects for any legitimately-formed
// request; the only cause observed in practice is an id argument (e.g. "/"
// or ".") that the server normalizes onto a different resource's path —
// following it would silently turn a single-object read into a collection
// read. That's the same failure class PathError guards, one layer up: the
// outbound request here is well-formed (PathError's check already passed),
// it's the server's *response* that redirects elsewhere, which is a shape
// PathError's request-construction check cannot see. Classified as
// exitUsage in cmd/errors.go, matching PathError, since every case observed
// so far is an id argument that didn't address a single resource.
type RedirectError struct {
	Method     string
	URL        string
	StatusCode int
	Location   string
}

func (e *RedirectError) Error() string {
	return fmt.Sprintf("refusing to follow redirect: %s %s returned %d to %q (an id argument that doesn't address a single resource is the only known cause)", e.Method, e.URL, e.StatusCode, e.Location)
}
