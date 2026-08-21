package cmd

import "testing"

// TestConnectorRowDeletedAtIsNullNotEmptyString pins that deleted_at is
// untyped nil, not "", on a live connector — "" is truthy in jq and would
// make `jq 'select(.deleted_at)'` match every row. mcp servers delete
// cascades into a connector's toolsets/tools, so being able to tell a
// deleted connector from a live one in a listing matters for audit.
func TestConnectorRowDeletedAtIsNullNotEmptyString(t *testing.T) {
	tests := []struct {
		name      string
		deletedAt string
		want      any
	}{
		{name: "live connector", deletedAt: "", want: nil},
		{name: "deleted connector", deletedAt: "2026-01-02T03:04:05Z", want: "2026-01-02T03:04:05Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := connectorListItem{}
			item.Connector.ID = "c1"
			item.Connector.DeletedAt = tt.deletedAt
			row := connectorRow(item)
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
