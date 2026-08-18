package cmd

import (
	"bytes"
	"errors"
	"sort"
	"strings"
	"testing"
)

// TestGuideNamesSorted pins that the advertised guide list is sorted and
// matches the registry keys exactly, so "docs guide" (no arg) and the error
// message on an unknown name never drift from what's actually registered.
func TestGuideNamesSorted(t *testing.T) {
	names := guideNames()
	if len(names) != len(docsGuides) {
		t.Fatalf("guideNames() returned %d names, want %d (one per registered guide)", len(names), len(docsGuides))
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("guideNames() = %v, want sorted order", names)
	}
	for _, n := range names {
		if _, ok := docsGuides[n]; !ok {
			t.Errorf("guideNames() returned %q, which is not a key of docsGuides", n)
		}
	}
}

// TestGuideRegistryLookup pins that every guide the task requires is
// registered with non-empty content, so a lookup by name never silently
// returns an empty runbook.
func TestGuideRegistryLookup(t *testing.T) {
	required := []string{"register-mcp-server", "assign-toolset-everyone", "test-mcp-gateway"}
	for _, name := range required {
		content, ok := docsGuides[name]
		if !ok {
			t.Errorf("expected guide %q to be registered", name)
			continue
		}
		if strings.TrimSpace(content) == "" {
			t.Errorf("guide %q has empty content", name)
		}
	}
}

// TestDocsGuideCmdKnownName runs the actual RunE for a known guide name and
// checks the guide content is written to stdout verbatim.
func TestDocsGuideCmdKnownName(t *testing.T) {
	cmd := docsGuideCmd
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.RunE(cmd, []string{"register-mcp-server"}); err != nil {
		t.Fatalf("RunE returned unexpected error: %v", err)
	}
	if buf.String() != guideRegisterMCPServer {
		t.Errorf("stdout did not match the registered guide content exactly")
	}
}

// TestDocsGuideCmdNoArgListsNames runs the actual RunE with no arguments and
// checks every registered guide name appears in the listing.
func TestDocsGuideCmdNoArgListsNames(t *testing.T) {
	cmd := docsGuideCmd
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE returned unexpected error: %v", err)
	}
	out := buf.String()
	for name := range docsGuides {
		if !strings.Contains(out, name) {
			t.Errorf("no-arg listing missing guide name %q; got:\n%s", name, out)
		}
	}
}

// TestDocsGuideCmdUnknownName pins the unknown-guide-name error: it must be a
// *usageError (so it maps to the documented exit code 2, per cmd/errors.go),
// and its message must name the requested guide.
func TestDocsGuideCmdUnknownName(t *testing.T) {
	cmd := docsGuideCmd
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.RunE(cmd, []string{"does-not-exist"})
	if err == nil {
		t.Fatal("expected an error for an unknown guide name, got nil")
	}

	var usageErr *usageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("expected a *usageError (exit code %d), got %T: %v", exitUsage, err, err)
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("error message %q does not mention the requested guide name", err.Error())
	}
	if got := exitCode(err); got != exitUsage {
		t.Errorf("exitCode(err) = %d, want %d (exitUsage)", got, exitUsage)
	}
}
