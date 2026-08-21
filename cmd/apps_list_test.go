package cmd

import (
	"encoding/json"
	"testing"
)

// TestAppRowUserCountIsNumeric pins that user_count keeps its real numeric
// type. The API sends it as a JSON string ("103"); emitting that string
// unchanged makes every string sort above every number in jq, so
// `jq 'select(.user_count > 60)'` would match every row regardless of value.
func TestAppRowUserCountIsNumeric(t *testing.T) {
	var item appListItem
	if err := json.Unmarshal([]byte(`{"id":"a1","userCount":"103"}`), &item); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	row := appRow(item)

	v, ok := row["user_count"].(int64)
	if !ok {
		t.Fatalf("user_count has type %T, want int64", row["user_count"])
	}
	if v != 103 {
		t.Errorf("user_count = %d, want 103", v)
	}
}

// TestAppRowDeletedAtIsNullNotEmptyString pins that deleted_at is untyped
// nil, not "", on a live app — "" is truthy in jq and would make
// `jq 'select(.deleted_at)'` match every row.
func TestAppRowDeletedAtIsNullNotEmptyString(t *testing.T) {
	tests := []struct {
		name      string
		deletedAt string
		want      any
	}{
		{name: "live app", deletedAt: "", want: nil},
		{name: "deleted app", deletedAt: "2026-01-02T03:04:05Z", want: "2026-01-02T03:04:05Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := appRow(appListItem{ID: "a1", DeletedAt: tt.deletedAt})
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
