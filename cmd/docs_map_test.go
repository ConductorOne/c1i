package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func runDocsMap(t *testing.T, args ...string) string {
	t.Helper()
	resetCmds(t, docsMapCmd)
	var out bytes.Buffer
	restoreCmdWriters(t)
	docsMapCmd.SetOut(&out)
	t.Cleanup(func() { docsMapCmd.SetOut(nil) })
	if err := docsMapCmd.ParseFlags(args); err != nil {
		t.Fatal(err)
	}
	if err := docsMapCmd.RunE(docsMapCmd, nil); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// TestDocsMapJSONMatchesTheTree pins the map to the command tree Cobra
// dispatches: every child path extends its parent, and the counts are real.
func TestDocsMapJSONMatchesTheTree(t *testing.T) {
	var doc commandMapDoc
	if err := json.Unmarshal([]byte(runDocsMap(t, "--json")), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if doc.Version == "" {
		t.Error("version is empty")
	}
	if doc.Counts.Families < 20 || doc.Counts.Commands < 200 || doc.Counts.Leaves < 200 {
		t.Fatalf("implausible counts: %+v", doc.Counts)
	}

	seen := map[string]bool{}
	leaves := 0
	var walk func(parent string, nodes []commandMapNode)
	walk = func(parent string, nodes []commandMapNode) {
		for _, n := range nodes {
			want := parent + " " + n.Name
			if n.Path != want {
				t.Errorf("path = %q, want %q", n.Path, want)
			}
			if seen[n.Path] {
				t.Errorf("duplicate path %q", n.Path)
			}
			seen[n.Path] = true
			if n.Short == "" && len(n.Children) == 0 {
				t.Errorf("%s has no description", n.Path)
			}
			if len(n.Children) == 0 {
				leaves++
				continue
			}
			walk(n.Path, n.Children)
		}
	}
	walk("c1i", doc.Families)
	if leaves != doc.Counts.Leaves {
		t.Errorf("leaves = %d, count says %d", leaves, doc.Counts.Leaves)
	}
	if len(seen) != doc.Counts.Commands {
		t.Errorf("commands = %d, count says %d", len(seen), doc.Counts.Commands)
	}
	if !seen["c1i requests create grant"] || !seen["c1i apps managed-state promote"] {
		t.Errorf("map is missing known commands: %v", seen)
	}
}

func TestDocsMapTextHasFamiliesAndLeafPaths(t *testing.T) {
	out := runDocsMap(t)
	for _, want := range []string{
		"c1i access-profiles — ",
		"c1i requests create grant",
		"c1i apps managed-state promote",
		"c1i docs map",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
	if strings.Contains(out, "c1i help") {
		t.Error("output includes the generated help command")
	}
}
