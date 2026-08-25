package cmd

import (
	"reflect"
	"testing"
)

// TestAppOwnerRowKeepsRealTypesAndOmitsEmptyDeletedAt pins the row
// projection GET .../owners feeds the emitter: strings pass through
// untouched, and deleted_at is untyped nil (not "") when the owner isn't
// deleted -- CLAUDE.md's row-fidelity rule (a "" would make
// `jq 'select(.deleted_at)'` match every live owner).
func TestAppOwnerRowKeepsRealTypesAndOmitsEmptyDeletedAt(t *testing.T) {
	cases := []struct {
		name string
		in   appOwner
		want map[string]any
	}{
		{
			name: "active owner, no deleted_at",
			in: appOwner{
				ID:          "u1",
				DisplayName: "Ada Lovelace",
				Email:       "ada@example.invalid",
				Username:    "ada",
				Status:      "ENABLED",
				JobTitle:    "Engineer",
			},
			want: map[string]any{
				"id":           "u1",
				"display_name": "Ada Lovelace",
				"email":        "ada@example.invalid",
				"username":     "ada",
				"status":       "ENABLED",
				"job_title":    "Engineer",
				"deleted_at":   nil,
			},
		},
		{
			name: "deleted owner keeps deleted_at populated",
			in: appOwner{
				ID:        "u2",
				Status:    "DELETED",
				DeletedAt: "2026-01-01T00:00:00Z",
			},
			want: map[string]any{
				"id":           "u2",
				"display_name": "",
				"email":        "",
				"username":     "",
				"status":       "DELETED",
				"job_title":    "",
				"deleted_at":   "2026-01-01T00:00:00Z",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := appOwnerRow(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("appOwnerRow(%+v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
