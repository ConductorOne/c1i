package cmd

import "testing"

// TestExecutionRowCompletedAtIsNullNotEmptyString pins that completed_at is
// untyped nil, not "", for a non-terminal execution (pending, creating,
// waiting) — "" is truthy in jq and would make `jq 'select(.completed_at)'`
// match every row, including ones that never finished.
func TestExecutionRowCompletedAtIsNullNotEmptyString(t *testing.T) {
	tests := []struct {
		name        string
		completedAt string
		want        any
	}{
		{name: "pending execution", completedAt: "", want: nil},
		{name: "completed execution", completedAt: "2026-01-02T03:04:05Z", want: "2026-01-02T03:04:05Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := executionRow(executionListItem{ID: "e1", CompletedAt: tt.completedAt})
			got, ok := row["completed_at"]
			if !ok {
				t.Fatal("row has no completed_at key")
			}
			if tt.want == nil {
				if got != nil {
					t.Fatalf("completed_at = %#v, want untyped nil", got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("completed_at = %v, want %v", got, tt.want)
			}
		})
	}
}
