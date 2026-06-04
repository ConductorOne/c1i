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
