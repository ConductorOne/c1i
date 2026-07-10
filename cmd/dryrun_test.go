package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestPrintDryRunWithBody(t *testing.T) {
	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	if err := printDryRun(cmd, "POST", "/api/v1/task/grant", map[string]any{"appId": "a1"}); err != nil {
		t.Fatalf("printDryRun: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "[dry-run] POST /api/v1/task/grant") {
		t.Errorf("missing request line in %q", out)
	}
	if !strings.Contains(out, `"appId": "a1"`) {
		t.Errorf("missing pretty-printed body in %q", out)
	}
}

func TestPrintDryRunNilBodyOmitsBody(t *testing.T) {
	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	if err := printDryRun(cmd, "DELETE", "/api/v1/apps/a/connectors/c/mcp_tools/t", nil); err != nil {
		t.Fatalf("printDryRun: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "[dry-run] DELETE /api/v1/apps/a/connectors/c/mcp_tools/t") {
		t.Errorf("missing request line in %q", out)
	}
	if strings.Contains(out, "{") {
		t.Errorf("nil body should not print any body, got %q", out)
	}
}

func TestParseKeyValueFlag(t *testing.T) {
	m, err := parseKeyValueFlag([]string{"A=1", "B=x=y"}, "header")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// A value may itself contain '=' — only the first '=' splits key from value.
	if m["A"] != "1" || m["B"] != "x=y" {
		t.Errorf("parsed = %v, want A=1 B=x=y", m)
	}

	if _, err := parseKeyValueFlag([]string{"nope"}, "header"); err == nil {
		t.Error("expected error for value with no '='")
	}
	if _, err := parseKeyValueFlag([]string{"=v"}, "header"); err == nil {
		t.Error("expected error for empty key")
	}

	m, err = parseKeyValueFlag(nil, "header")
	if err != nil || m != nil {
		t.Errorf("empty input should return (nil, nil), got (%v, %v)", m, err)
	}
}
