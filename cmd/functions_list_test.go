package cmd

import "testing"

// TestFunctionRowPublishedCommitIDIsNullNotEmptyString pins that
// published_commit_id is untyped nil, not "", for a function with no
// published commit — "" is truthy in jq and would make
// `jq 'select(.published_commit_id)'` match every row.
func TestFunctionRowPublishedCommitIDIsNullNotEmptyString(t *testing.T) {
	tests := []struct {
		name     string
		commitID string
		want     any
	}{
		{name: "unpublished", commitID: "", want: nil},
		{name: "published", commitID: "commit-123", want: "commit-123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := functionRow(functionListItem{ID: "f1", PublishedCommitID: tt.commitID})
			got, ok := row["published_commit_id"]
			if !ok {
				t.Fatal("row has no published_commit_id key")
			}
			if tt.want == nil {
				if got != nil {
					t.Fatalf("published_commit_id = %#v, want untyped nil", got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("published_commit_id = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestFunctionRowDeletedAtIsNullNotEmptyString pins that deleted_at is
// untyped nil, not "", on a live function.
func TestFunctionRowDeletedAtIsNullNotEmptyString(t *testing.T) {
	tests := []struct {
		name      string
		deletedAt string
		want      any
	}{
		{name: "live function", deletedAt: "", want: nil},
		{name: "deleted function", deletedAt: "2026-01-02T03:04:05Z", want: "2026-01-02T03:04:05Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := functionRow(functionListItem{ID: "f1", DeletedAt: tt.deletedAt})
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
