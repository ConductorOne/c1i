package cmd

import "testing"

// mapToolState and mapToolClassification are case-insensitive convenience
// translations from user-friendly --state / --classification values to the
// prefixed API enums. Pin them so a future rename or typo in the proto
// surface is caught here before it ships.
func TestMapToolState(t *testing.T) {
	cases := map[string]string{
		"pending":        "MCP_TOOL_STATE_PENDING_REVIEW",
		"pending_review": "MCP_TOOL_STATE_PENDING_REVIEW",
		"PENDING":        "MCP_TOOL_STATE_PENDING_REVIEW",
		"approved":       "MCP_TOOL_STATE_APPROVED",
		"Approved":       "MCP_TOOL_STATE_APPROVED",
		"disabled":       "MCP_TOOL_STATE_DISABLED",
		"removed":        "MCP_TOOL_STATE_REMOVED",
		"":               "",
		"unknown":        "unknown",
	}
	for in, want := range cases {
		if got := mapToolState(in); got != want {
			t.Errorf("mapToolState(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMapToolClassification(t *testing.T) {
	cases := map[string]string{
		"read":        "TOOL_CLASSIFICATION_READ",
		"READ":        "TOOL_CLASSIFICATION_READ",
		"write":       "TOOL_CLASSIFICATION_WRITE",
		"destructive": "TOOL_CLASSIFICATION_DESTRUCTIVE",
		"sensitive":   "TOOL_CLASSIFICATION_SENSITIVE",
		"dangerous":   "TOOL_CLASSIFICATION_DANGEROUS",
		"":            "",
		"unknown":     "unknown",
	}
	for in, want := range cases {
		if got := mapToolClassification(in); got != want {
			t.Errorf("mapToolClassification(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFirstNonEmpty(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"first wins", []string{"a", "b"}, "a"},
		{"fallback to second", []string{"", "b"}, "b"},
		{"all empty", []string{"", ""}, ""},
		{"no args", nil, ""},
	}
	for _, tc := range cases {
		if got := firstNonEmpty(tc.in...); got != tc.want {
			t.Errorf("%s: firstNonEmpty(%v) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

// TestToolRowDeletedAtIsNullNotEmptyString pins that deleted_at is untyped
// nil, not "", on a live tool. "mcp servers delete" cascades into a server's
// tools, so this is what would let a listing distinguish a cascaded-deleted
// tool from a live one.
func TestToolRowDeletedAtIsNullNotEmptyString(t *testing.T) {
	tests := []struct {
		name      string
		deletedAt string
		want      any
	}{
		{name: "live tool", deletedAt: "", want: nil},
		{name: "deleted tool", deletedAt: "2026-01-02T03:04:05Z", want: "2026-01-02T03:04:05Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := toolRow(toolView{ID: "t1", DeletedAt: tt.deletedAt})
			got, ok := row["deleted_at"]
			if !ok {
				t.Fatal("row has no deleted_at key")
			}
			if tt.want == nil {
				if got != nil {
					t.Fatalf("deleted_at = %#v, want untyped nil", got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("deleted_at = %v, want %v", got, tt.want)
			}
		})
	}
}
