package transport

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

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
// The host check exists because this tripper sits outside any credential-
// attaching transport (an oauth2 transport, or a plain Authorization header
// set by the caller): following to an arbitrary host would hand that host
// the caller's credentials, which is exactly what net/http's own redirect
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
// and Do()'s guard; a trailing-slash difference therefore counts as
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
