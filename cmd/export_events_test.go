package cmd

import (
	"reflect"
	"testing"
)

func TestMapSortDirection(t *testing.T) {
	cases := map[string]struct {
		want    string
		wantErr bool
	}{
		"":       {want: "SORT_DIRECTION_ASC"},
		"asc":    {want: "SORT_DIRECTION_ASC"},
		"ASC":    {want: "SORT_DIRECTION_ASC"},
		"desc":   {want: "SORT_DIRECTION_DESC"},
		" Desc ": {want: "SORT_DIRECTION_DESC"},
		"newest": {wantErr: true},
	}
	for in, tc := range cases {
		got, err := mapSortDirection(in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("mapSortDirection(%q) = %q, want error", in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("mapSortDirection(%q) unexpected error: %v", in, err)
		}
		if got != tc.want {
			t.Errorf("mapSortDirection(%q) = %q, want %q", in, got, tc.want)
		}
	}
}

func TestExportEventsBody(t *testing.T) {
	// Minimal body: only pageSize; optional filters omitted when empty.
	got := exportEventsBody(50, "", "", "", "", "")
	if !reflect.DeepEqual(got, map[string]any{"pageSize": 50}) {
		t.Errorf("minimal body = %v, want just pageSize", got)
	}

	// Full body: every filter present.
	got = exportEventsBody(25, "tok", "2026-07-01T00:00:00Z", "2026-07-08T00:00:00Z", "evt-1", "SORT_DIRECTION_DESC")
	want := map[string]any{
		"pageSize":      25,
		"pageToken":     "tok",
		"since":         "2026-07-01T00:00:00Z",
		"until":         "2026-07-08T00:00:00Z",
		"sinceEventUid": "evt-1",
		"sortDirection": "SORT_DIRECTION_DESC",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("full body = %v, want %v", got, want)
	}
}

func TestValidateRFC3339(t *testing.T) {
	if err := validateRFC3339("--since", ""); err != nil {
		t.Errorf("empty timestamp should be allowed, got %v", err)
	}
	if err := validateRFC3339("--since", "2026-07-01T00:00:00Z"); err != nil {
		t.Errorf("valid RFC3339 rejected: %v", err)
	}
	if err := validateRFC3339("--since", "2026-07-01"); err == nil {
		t.Error("expected error for non-RFC3339 timestamp, got nil")
	}
}
