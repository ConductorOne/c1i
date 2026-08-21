package transport

import "testing"

func TestPathHasEmptySegment(t *testing.T) {
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
			if got := pathHasEmptySegment(tc.path); got != tc.want {
				t.Errorf("pathHasEmptySegment(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}
