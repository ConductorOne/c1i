package cmd

import "testing"

// TestToolsetRowDeletedAtIsNullNotEmptyString pins that deleted_at is untyped
// nil, not "", on a live toolset. "mcp servers delete" cascades into a
// server's toolsets, so this is what would let a listing distinguish a
// cascaded-deleted toolset from a live one.
func TestToolsetRowDeletedAtIsNullNotEmptyString(t *testing.T) {
	tests := []struct {
		name      string
		deletedAt string
		want      any
	}{
		{name: "live toolset", deletedAt: "", want: nil},
		{name: "deleted toolset", deletedAt: "2026-01-02T03:04:05Z", want: "2026-01-02T03:04:05Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := toolsetRow(toolsetView{ID: "t1", DeletedAt: tt.deletedAt})
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
