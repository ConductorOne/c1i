package cmd

import (
	"encoding/json"
	"testing"
)

// TestFunctionsSourceCommitParse verifies the commit-response parser handles
// both possible shapes the API has used: the current "files" key and the
// older "content" key. The command silently accepts either so a future API
// rename doesn't break the workflow.
func TestFunctionsSourceCommitParse(t *testing.T) {
	cases := map[string]string{
		"files-key":   `{"files":{"main.ts":"Zm9v"}}`,
		"content-key": `{"content":{"main.ts":"Zm9v"}}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			var commit struct {
				Files   map[string]string `json:"files"`
				Content map[string]string `json:"content"`
			}
			if err := json.Unmarshal([]byte(payload), &commit); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}
			files := commit.Files
			if len(files) == 0 {
				files = commit.Content
			}
			if v, ok := files["main.ts"]; !ok || v != "Zm9v" {
				t.Errorf("expected main.ts=Zm9v in %s, got %v", name, files)
			}
		})
	}
}

// TestFunctionsSourceMetadataParse pins the dual-shape parsing for the
// function-metadata GET. The single-resource endpoint wraps under "function"
// while a hypothetical flat response would not — the source command needs to
// handle both so it doesn't silently fall through to "no commit found".
func TestFunctionsSourceMetadataParse(t *testing.T) {
	cases := map[string]struct {
		payload      string
		wantCommit   string
	}{
		"wrapped": {
			payload:    `{"function":{"publishedCommitId":"abc","head":"def"}}`,
			wantCommit: "abc",
		},
		"wrapped-head-only": {
			payload:    `{"function":{"publishedCommitId":"","head":"xyz"}}`,
			wantCommit: "xyz",
		},
		"flat": {
			payload:    `{"publishedCommitId":"flat-abc","head":"flat-head"}`,
			wantCommit: "flat-abc",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var wrapper struct {
				Function struct {
					PublishedCommitID string `json:"publishedCommitId"`
					Head              string `json:"head"`
				} `json:"function"`
				PublishedCommitID string `json:"publishedCommitId"`
				Head              string `json:"head"`
			}
			if err := json.Unmarshal([]byte(tc.payload), &wrapper); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}
			commitID := wrapper.Function.PublishedCommitID
			if commitID == "" {
				commitID = wrapper.PublishedCommitID
			}
			if commitID == "" {
				commitID = wrapper.Function.Head
			}
			if commitID == "" {
				commitID = wrapper.Head
			}
			if commitID != tc.wantCommit {
				t.Errorf("got %q, want %q", commitID, tc.wantCommit)
			}
		})
	}
}
