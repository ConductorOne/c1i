package cmd

import (
	"encoding/json"
	"testing"
)

// TestEntitlementRowGrantCountIsNumeric pins that grant_count keeps its real
// numeric type. The API sends it as a JSON string ("88"); emitting that
// string unchanged makes every string sort above every number in jq, so
// `jq 'select(.grant_count > 60)'` would match every row regardless of value.
func TestEntitlementRowGrantCountIsNumeric(t *testing.T) {
	var item entitlementListItem
	raw := `{"appEntitlement":{"id":"e1","grantCount":"88"}}`
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	row := entitlementRow(item)

	v, ok := row["grant_count"].(int64)
	if !ok {
		t.Fatalf("grant_count has type %T, want int64", row["grant_count"])
	}
	if v != 88 {
		t.Errorf("grant_count = %d, want 88", v)
	}
}

// TestEntitlementRowDeletedAtIsNullNotEmptyString pins that deleted_at is
// untyped nil, not "", on a live entitlement.
func TestEntitlementRowDeletedAtIsNullNotEmptyString(t *testing.T) {
	tests := []struct {
		name      string
		deletedAt string
		want      any
	}{
		{name: "live entitlement", deletedAt: "", want: nil},
		{name: "deleted entitlement", deletedAt: "2026-01-02T03:04:05Z", want: "2026-01-02T03:04:05Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := entitlementListItem{}
			item.AppEntitlement.ID = "e1"
			item.AppEntitlement.DeletedAt = tt.deletedAt
			row := entitlementRow(item)
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
