package transport

import "strings"

// pathHasEmptySegment reports whether path contains an empty segment: a
// trailing slash after content, or an interior "//". Only the path is
// examined — a query string or fragment suffix ("?..." / "#...") is stripped
// first, so neither can mask or manufacture a false positive. A bare "/" is
// not flagged: it's a single root segment, not one carved out by a missing
// id, and no request in this codebase ever targets just "/".
func pathHasEmptySegment(path string) bool {
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
	}
	return false
}
