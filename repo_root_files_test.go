package main

import (
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"
)

// allowedRootFiles lists every file permitted at the repository root.
//
// It is an allowlist rather than a pattern of known-bad names because the
// failure it guards is an unexpected file *arriving*: a release commit once
// swept 18 scratch files into this repo -- command output redirected into the
// working directory, picked up by `git add -A` -- and no name pattern would
// have predicted them. Scratch output lands wherever the command ran, which is
// almost always here. Adding a root file is rare and deliberate; adding it to
// this list should be too.
var allowedRootFiles = map[string]bool{
	".gitignore":              true,
	".golangci.yml":           true,
	".goreleaser.yaml":        true,
	"CHANGELOG.md":            true,
	"CLAUDE.md":               true,
	"LICENSE":                 true,
	"README.md":               true,
	"go.mod":                  true,
	"go.sum":                  true,
	"main.go":                 true,
	"repo_root_files_test.go": true,
}

func TestNoUnexpectedFilesAtRepoRoot(t *testing.T) {
	// Distinguish "no checkout to inspect" from "git is broken": skipping on any
	// git failure would silently downgrade this guard to a passing no-op, which
	// reads as coverage.
	if _, err := os.Stat(".git"); err != nil {
		t.Skipf("not a git checkout (%v), so there is nothing to inspect", err)
	}
	out, err := exec.Command("git", "ls-files", "-z").Output()
	if err != nil {
		t.Fatalf("git ls-files failed inside a git checkout: %v", err)
	}

	var unexpected []string
	for _, path := range strings.Split(string(out), "\x00") {
		if path == "" || strings.Contains(path, "/") {
			continue
		}
		if !allowedRootFiles[path] {
			unexpected = append(unexpected, path)
		}
	}
	sort.Strings(unexpected)

	if len(unexpected) > 0 {
		t.Errorf("unexpected file(s) tracked at the repository root: %s\n"+
			"If this is scratch output or test debris, remove it from the commit "+
			"(`git rm --cached <file>`) -- it would otherwise ship in a public "+
			"repository.\nIf it genuinely belongs here, add it to "+
			"allowedRootFiles in this file.", strings.Join(unexpected, ", "))
	}
}
