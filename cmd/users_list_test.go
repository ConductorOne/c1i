package cmd

import "testing"

// TestMapUserStatus pins the mapping between the user-friendly --status flag
// values and the bare enum values the search/users API actually accepts.
//
// History: a previous version mapped to STATUS_ENABLED / STATUS_DISABLED /
// STATUS_DELETED, which the API silently rejected with a 400. The API
// expects bare ENABLED / DISABLED / DELETED for users (the app_users API
// uses the prefixed form, which is a separate enum — don't conflate them).
func TestMapUserStatus(t *testing.T) {
	cases := map[string]string{
		"enabled":  "ENABLED",
		"disabled": "DISABLED",
		"deleted":  "DELETED",
		"":         "", // empty passes through
		"unknown":  "unknown",
	}
	for in, want := range cases {
		if got := mapUserStatus(in); got != want {
			t.Errorf("mapUserStatus(%q) = %q, want %q", in, got, want)
		}
	}
}
