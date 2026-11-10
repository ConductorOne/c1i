package transport

import (
	"net/url"
	"strings"
)

// pathHasBadSegment reports whether path contains a segment that makes the
// server address something other than what was named: an empty segment (a
// trailing slash after content, or an interior "//"), or a "." / ".." dot
// segment (ignoring any ";params" suffix and surrounding whitespace), which
// the server normalizes to the parent or collection -- for a non-GET with no
// redirect for the redirect guard to see. Each segment is percent-decoded
// before the dot check because the server decodes %2E before normalizing. A
// query or fragment suffix is stripped first. A bare "/" is not flagged: no
// request in this codebase targets just "/".
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
		if IsDotSegmentID(seg) {
			return true
		}
	}
	return false
}

// IsDotSegmentID reports whether s, used as one path segment (an id), is a
// "." or ".." dot segment: the server percent-decodes, strips a ";params"
// suffix and trims surrounding whitespace before resolving dots, so "%2E",
// "..;x" and " .. " all count. An undecodable value is not a dot segment (it
// reaches the server verbatim).
//
// Exported for the CLI's pre-send validation: a body-carried id never reaches
// a request path this guard can inspect, so the same rule has to run on the
// value itself -- otherwise --dry-run prints it as if it were an id.
func IsDotSegmentID(s string) bool {
	dec, err := url.PathUnescape(s)
	if err != nil {
		return false
	}
	// The server drops ";params" and trims before resolving dots, so "..;x"
	// and " .. " are both "..".
	if i := strings.IndexByte(dec, ';'); i >= 0 {
		dec = dec[:i]
	}
	dec = strings.TrimSpace(dec)
	return dec == "." || dec == ".."
}
