package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
		payload    string
		wantCommit string
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

// TestUnsafeSourceName pins the --out-dir filename guard: plain filenames are
// accepted, anything that could escape the output directory is rejected. The
// filename originates from the API response, so this is the defense against a
// hostile or buggy server writing outside --out-dir.
func TestUnsafeSourceName(t *testing.T) {
	safe := []string{"main.ts", "main.test.ts", "a.b.c.ts", "README"}
	for _, n := range safe {
		if unsafeSourceName(n) {
			t.Errorf("expected %q to be safe", n)
		}
	}
	unsafe := []string{
		"", ".", "..",
		"../etc/passwd",
		"../../etc/cron.d/x",
		"sub/main.ts",
		"/etc/passwd",
		"a/../../b",
	}
	for _, n := range unsafe {
		if !unsafeSourceName(n) {
			t.Errorf("expected %q to be rejected", n)
		}
	}
}

// TestOutDirFlagHelpDocumentsModes guards against the flag help drifting
// from what --out-dir actually does: files land at 0600, the directory is
// held to at most 0750, because fetched source may inline credentials.
func TestOutDirFlagHelpDocumentsModes(t *testing.T) {
	usage := functionsSourceCmd.Flags().Lookup("out-dir").Usage
	for _, want := range []string{"0600", "0750", "credentials"} {
		if !strings.Contains(usage, want) {
			t.Errorf("--out-dir help %q does not mention %q", usage, want)
		}
	}
}

// statPerm returns the permission bits of path, failing the test on error.
func statPerm(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode().Perm()
}

// TestHardenOutDirCreatesFresh proves a directory that does not exist yet is
// created at 0750, with no warning (there is nothing to tighten).
func TestHardenOutDirCreatesFresh(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fresh")
	var warn bytes.Buffer
	if err := hardenOutDir(dir, &warn); err != nil {
		t.Fatalf("hardenOutDir: %v", err)
	}
	if got := statPerm(t, dir); got != 0o750 {
		t.Errorf("fresh dir mode = %o, want 0750", got)
	}
	if warn.Len() != 0 {
		t.Errorf("expected no warning for a freshly created dir, got %q", warn.String())
	}
}

// TestHardenOutDirTightensPreExisting proves a pre-existing directory that is
// more permissive than 0750 (e.g. 0777, mkdir+chmod'd by a script before the
// first run) is tightened on a later run, not left as-is — this is the bug:
// MkdirAll's mode argument is a no-op on a path that already exists.
func TestHardenOutDirTightensPreExisting(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(dir, 0o777); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var warn bytes.Buffer
	if err := hardenOutDir(dir, &warn); err != nil {
		t.Fatalf("hardenOutDir: %v", err)
	}
	if got := statPerm(t, dir); got != 0o750 {
		t.Errorf("pre-existing 0777 dir mode after harden = %o, want 0750", got)
	}
	if warn.Len() == 0 {
		t.Errorf("expected a warning when tightening a pre-existing dir's permissions")
	}
	if !strings.Contains(warn.String(), dir) {
		t.Errorf("warning %q does not name the tightened directory %q", warn.String(), dir)
	}
}

// TestHardenOutDirNeverWidens proves a pre-existing directory already
// stricter than 0750 (e.g. 0700, owner-only) is left untouched — hardenOutDir
// must never loosen permissions, only tighten them.
func TestHardenOutDirNeverWidens(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "strict")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var warn bytes.Buffer
	if err := hardenOutDir(dir, &warn); err != nil {
		t.Fatalf("hardenOutDir: %v", err)
	}
	if got := statPerm(t, dir); got != 0o700 {
		t.Errorf("strict 0700 dir mode after harden = %o, want unchanged 0700", got)
	}
	if warn.Len() != 0 {
		t.Errorf("expected no warning for a dir already stricter than 0750, got %q", warn.String())
	}
}

// TestHardenOutDirLeavesExactModeAlone proves a pre-existing directory
// already exactly at 0750 is left alone with no spurious warning.
func TestHardenOutDirLeavesExactModeAlone(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "exact")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var warn bytes.Buffer
	if err := hardenOutDir(dir, &warn); err != nil {
		t.Fatalf("hardenOutDir: %v", err)
	}
	if got := statPerm(t, dir); got != 0o750 {
		t.Errorf("exact 0750 dir mode after harden = %o, want unchanged 0750", got)
	}
	if warn.Len() != 0 {
		t.Errorf("expected no warning for a dir already at 0750, got %q", warn.String())
	}
}
