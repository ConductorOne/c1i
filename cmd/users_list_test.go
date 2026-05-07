package cmd

import "testing"

// All four enum-mapping functions are case-insensitive. Pin them all so
// the next refactor catches a regression before it ships.

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
		"ENABLED":  "ENABLED",  // case-insensitive
		"Disabled": "DISABLED", // mixed case
		"":         "",         // empty passes through
		"unknown":  "unknown",
	}
	for in, want := range cases {
		if got := mapUserStatus(in); got != want {
			t.Errorf("mapUserStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMapTaskState(t *testing.T) {
	cases := map[string]string{
		"open":    "TASK_STATE_OPEN",
		"closed":  "TASK_STATE_CLOSED",
		"OPEN":    "TASK_STATE_OPEN",  // case-insensitive
		"Closed":  "TASK_STATE_CLOSED",
		"":        "",
		"unknown": "unknown",
	}
	for in, want := range cases {
		if got := mapTaskState(in); got != want {
			t.Errorf("mapTaskState(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMapAppUserStatus(t *testing.T) {
	cases := map[string]string{
		"enabled":  "STATUS_ENABLED",
		"disabled": "STATUS_DISABLED",
		"deleted":  "STATUS_DELETED",
		"ENABLED":  "STATUS_ENABLED",
		"Disabled": "STATUS_DISABLED",
		"":         "",
		"unknown":  "unknown",
	}
	for in, want := range cases {
		if got := mapAppUserStatus(in); got != want {
			t.Errorf("mapAppUserStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMapAppUserType(t *testing.T) {
	cases := map[string]string{
		"user":            "APP_USER_TYPE_USER",
		"service_account": "APP_USER_TYPE_SERVICE_ACCOUNT",
		"system_account":  "APP_USER_TYPE_SYSTEM_ACCOUNT",
		"USER":            "APP_USER_TYPE_USER",
		"Service_Account": "APP_USER_TYPE_SERVICE_ACCOUNT",
		"":                "",
		"unknown":         "unknown",
	}
	for in, want := range cases {
		if got := mapAppUserType(in); got != want {
			t.Errorf("mapAppUserType(%q) = %q, want %q", in, got, want)
		}
	}
}
