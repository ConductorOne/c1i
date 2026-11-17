// Package transport is the auth-independent HTTP layer beneath c1i's
// authenticated API client: timeout, retry/backoff, user-agent, debug
// tracing, the empty-path-segment guard, and the redirect trust-scope guard.
// It knows nothing about OAuth, credentials, or the C1 API's own shape, so
// any package that speaks HTTP to a C1 host -- authenticated or not -- can
// depend on it without a cycle.
package transport

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/redact"
)

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
	// PriorSent: an earlier, retried attempt reached the server.
	PriorSent bool
}

func (e *APIError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("API %s %s returned %d", e.Method, redact.Path(e.Path), e.StatusCode)
	}
	return fmt.Sprintf("API %s %s returned %d: %s", e.Method, redact.Path(e.Path), e.StatusCode, redact.Body(e.Body))
}

// PathError is returned when a request's path contains an empty segment (a
// trailing slash or an interior "//") or a "." / ".." segment, most commonly
// from an id argument of "", ".", or "..", which client.Path passes through
// unchanged. The server resolves each to the parent or collection, so the
// request would otherwise read or mutate the wrong resource. Do() rejects it
// before any request is sent.
type PathError struct {
	Method string
	Path   string
}

func (e *PathError) Error() string {
	return fmt.Sprintf(`refusing to send %s %s: empty path segment or "."/".." segment (an id argument was likely "", ".", or "..")`, e.Method, redact.Path(e.Path))
}

// RedirectError is returned when a 3xx response's target changes the
// request's path, or keeps the path but points at a host outside the
// request host's trust scope (see redirectTripper and hostInScope); the host
// restriction keeps a same-path canonicalization hop from carrying the
// caller's credentials to an unrelated host. It also covers the remaining
// refusals resolveAllowedRedirect makes: an https->http scheme downgrade, and
// an empty or unparseable Location. Reason names which of those rules fired
// so the message doesn't blame a path change that never happened.
type RedirectError struct {
	Method     string
	URL        string
	StatusCode int
	Location   string
	// Reason is the rule that refused the hop; the zero value means the
	// caller didn't say (an error assembled from the structured fields
	// alone), in which case Error() names every rule rather than guessing.
	Reason RedirectReason
}

// RedirectReason identifies the rule in resolveAllowedRedirect that refused
// a redirect hop.
type RedirectReason uint8

const (
	// RedirectReasonUnknown is the zero value: no rule was recorded.
	RedirectReasonUnknown RedirectReason = iota
	// RedirectReasonPath: the target's escaped path differs from the
	// request's (including a trailing-slash difference).
	RedirectReasonPath
	// RedirectReasonHost: the path is unchanged but the target host is
	// outside the request host's trust scope.
	RedirectReasonHost
	// RedirectReasonScheme: the path and host would pass, but an https
	// request would be redirected to a non-https target.
	RedirectReasonScheme
	// RedirectReasonEmptyLocation: the 3xx carried no Location.
	RedirectReasonEmptyLocation
	// RedirectReasonBadLocation: the Location could not be parsed or
	// resolved against the request URL.
	RedirectReasonBadLocation
)

// rule returns the sentence naming the rule r refused under.
func (r RedirectReason) rule() string {
	switch r {
	case RedirectReasonPath:
		return "a redirect that changes the request path is never followed"
	case RedirectReasonHost:
		return "a redirect that points at a host outside the request host's trust scope is never followed"
	case RedirectReasonScheme:
		return "a redirect that takes an https request to a non-https target is never followed"
	case RedirectReasonEmptyLocation:
		return "a redirect with an empty Location is never followed"
	case RedirectReasonBadLocation:
		return `a redirect whose Location cannot be parsed or resolved is never followed`
	default:
		return "a redirect that changes the request path, host or scheme is never followed"
	}
}

func (e *RedirectError) Error() string {
	return fmt.Sprintf("refusing to follow redirect: %s %s returned %d to %q (%s)", e.Method, e.URL, e.StatusCode, e.Location, e.Reason.rule())
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
