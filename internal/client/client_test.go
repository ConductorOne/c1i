package client

import "testing"

func TestPath(t *testing.T) {
	tests := []struct {
		name   string
		format string
		ids    []string
		want   string
	}{
		{
			name:   "single segment",
			format: "/api/v1/functions/%s",
			ids:    []string{"abc123"},
			want:   "/api/v1/functions/abc123",
		},
		{
			name:   "multiple segments",
			format: "/api/v1/apps/%s/connectors/%s/mcp_tools/%s",
			ids:    []string{"app1", "conn2", "tool3"},
			want:   "/api/v1/apps/app1/connectors/conn2/mcp_tools/tool3",
		},
		{
			name:   "escapes reserved characters",
			format: "/api/v1/tasks/%s/action/deny",
			ids:    []string{"a?b#c d"},
			want:   "/api/v1/tasks/a%3Fb%23c%20d/action/deny",
		},
		{
			name:   "escapes slashes so a value cannot traverse segments",
			format: "/api/v1/functions/%s",
			ids:    []string{"a/b"},
			want:   "/api/v1/functions/a%2Fb",
		},
		{
			name:   "no ids returns format unchanged",
			format: "/api/v1/apps",
			ids:    nil,
			want:   "/api/v1/apps",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Path(tt.format, tt.ids...); got != tt.want {
				t.Errorf("Path(%q, %v) = %q, want %q", tt.format, tt.ids, got, tt.want)
			}
		})
	}
}

func TestPathLiteralPercent(t *testing.T) {
	// %% is a literal percent, not a verb, so it needs no id.
	if got := Path("/api/v1/x/%s/y%%z", "id"); got != "/api/v1/x/id/y%z" {
		t.Errorf("Path with literal %%%% = %q", got)
	}
}

func TestPathPanicsOnMismatch(t *testing.T) {
	cases := []struct {
		name   string
		format string
		ids    []string
	}{
		{"too few ids", "/api/v1/apps/%s/connectors/%s", []string{"a"}},
		{"too many ids", "/api/v1/apps/%s", []string{"a", "b"}},
		{"unsupported verb", "/api/v1/apps/%d", []string{"a"}},
		{"dangling percent", "/api/v1/apps/%", []string{"a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("Path(%q, %v) did not panic", tc.format, tc.ids)
				}
			}()
			_ = Path(tc.format, tc.ids...)
		})
	}
}
