package transport

import "testing"

func TestPathHasBadSegment(t *testing.T) {
	cases := []struct {
		name string
		path string
		want bool
	}{
		// The bug this guards against: a trailing empty segment from an empty
		// id, with and without a query/fragment tacked on.
		{"trailing slash", "/api/v1/policies/", true},
		{"trailing slash with query", "/api/v1/policies/?x=1", true},
		{"trailing slash with fragment", "/api/v1/policies/#frag", true},
		{"interior double slash", "/api/v1//policies", true},
		{"interior double slash with query", "/api/v1//policies?x=1", true},

		// Dot segments: the server normalizes these (decoding %2E first), so
		// they address the parent or collection instead of the named id.
		{"dot segment", "/api/v1/users/.", true},
		{"dot-dot segment", "/api/v1/users/..", true},
		{"interior dot segment", "/api/v1/webhooks/./test", true},
		{"interior dot-dot segment", "/api/v1/apps/../users", true},
		{"encoded dot", "/api/v1/users/%2E", true},
		{"encoded dot lowercase", "/api/v1/users/%2e", true},
		{"encoded dot-dot mixed case", "/api/v1/users/%2e%2E", true},
		{"half-encoded dot-dot", "/api/v1/users/.%2E", true},
		{"trailing dot segment", "/api/v1/apps/abc123/.", true},
		{"dot segment with query", "/api/v1/users/.?x=1", true},

		// Matrix params: the server strips ";..." before resolving dots.
		{"dot-dot matrix", "/api/v1/users/x/..;/abc", true},
		{"dot-dot matrix value", "/api/v1/users/x/..;x/abc", true},
		{"dot matrix", "/api/v1/users/.;", true},
		{"encoded dot-dot matrix", "/api/v1/users/%2E%2E;", true},
		{"dot-dot encoded semicolon", "/api/v1/users/..%3B", true},
		{"triple dot matrix id", "/api/v1/users/...;", false},
		{"semicolon id", "/api/v1/users/a;b", false},

		// Ids that merely contain dots are legitimate.
		{"dotted id", "/api/v1/users/a.b", false},
		{"leading dot-dot id", "/api/v1/users/..a", false},
		{"version-like id", "/api/v1/users/v1.2", false},
		{"triple dot id", "/api/v1/users/...", false},
		{"trailing dot id", "/api/v1/users/a.", false},
		{"encoded triple dot id", "/api/v1/users/%2E%2E%2E", false},

		// Query/fragment alone must never trip the detector.
		{"path with query, no trailing slash", "/api/v1/policies?page_size=5", false},
		{"path with fragment, no trailing slash", "/api/v1/policies#frag", false},

		// Edge cases.
		{"bare root", "/", false},
		{"empty string", "", false},

		// Real paths from this codebase (cmd/*.go client.Path calls) that must
		// never be flagged.
		{"real: apps get", "/api/v1/apps/abc123", false},
		{"real: nested ids", "/api/v1/apps/abc123/app_users/def456", false},
		{"real: collection action, no id", "/api/v1/apps/abc123/connectors/def456/mcp_tools/search", false},
		{"real: bare collection", "/api/v1/policies", false},
		{"real: tasks action", "/api/v1/tasks/abc123/action/approve", false},
		{"real: deep nested action", "/api/v1/users/abc123/mcp_toolsets/requestable_connectors", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pathHasBadSegment(tc.path); got != tc.want {
				t.Errorf("pathHasBadSegment(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}
