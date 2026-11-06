package transport

import (
	"net/url"
	"strings"
)

// pathHasBadSegment reports whether path contains a segment that makes the
// server address something other than what was named: an empty segment (a
// trailing slash after content, or an interior "//"), or a "." / ".." dot
// segment (ignoring any ";params" suffix), which the server normalizes to the
// parent or collection -- for a non-GET with no redirect for the redirect
// guard to see. Each segment is
// percent-decoded before the dot check because the server decodes %2E before
// normalizing. A query or fragment suffix is stripped first. A bare "/" is
// not flagged: no request in this codebase targets just "/".
func pathHasBadSegment(path string) bool {
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	if path == "" || path == "/" {
		return false
	}
	for _, seg := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if seg == "" {
			return true
		}
		dec, err := url.PathUnescape(seg)
		if err != nil {
			continue
		}
		// The server drops ";params" before resolving dots, so "..;x" is "..".
		if i := strings.IndexByte(dec, ';'); i >= 0 {
			dec = dec[:i]
		}
		if dec == "." || dec == ".." {
			return true
		}
	}
	return false
}
