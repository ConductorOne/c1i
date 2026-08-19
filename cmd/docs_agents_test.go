package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runDocsAgents drives docsAgentsCmd.RunE directly (no auth, no network)
// with the given args and returns stdout/stderr.
func runDocsAgents(t *testing.T, args []string) (stdout, stderr string) {
	t.Helper()

	cmd := docsAgentsCmd
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(outBuf)
	cmd.SetErr(errBuf)
	if err := cmd.Flags().Set("output", ""); err != nil {
		t.Fatalf("failed to reset --output: %v", err)
	}

	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("ParseFlags failed: %v", err)
	}
	remaining := cmd.Flags().Args()
	if cmd.Args != nil {
		if err := cmd.Args(cmd, remaining); err != nil {
			return outBuf.String(), err.Error()
		}
	}
	if err := cmd.RunE(cmd, remaining); err != nil {
		return outBuf.String(), err.Error()
	}
	return outBuf.String(), errBuf.String()
}

// TestDocsAgentsNoVersionPlaceholder guards against a literal "{{VERSION}}"
// leaking into agent-facing output if the substitution is ever dropped.
func TestDocsAgentsNoVersionPlaceholder(t *testing.T) {
	stdout, _ := runDocsAgents(t, nil)
	if strings.Contains(stdout, "{{VERSION}}") {
		t.Errorf("docs agents output still contains the literal {{VERSION}} placeholder")
	}
}

// TestDocsAgentsOutputFileMatchesStdout proves "-o FILE" writes byte-identical
// content to what "docs agents" prints on stdout.
func TestDocsAgentsOutputFileMatchesStdout(t *testing.T) {
	stdout, _ := runDocsAgents(t, nil)

	dir := t.TempDir()
	outPath := filepath.Join(dir, "AGENTS.md")

	cmd := docsAgentsCmd
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(outBuf)
	cmd.SetErr(errBuf)
	if err := cmd.Flags().Set("output", outPath); err != nil {
		t.Fatalf("failed to set --output: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Flags().Set("output", "")
	})
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE with --output returned unexpected error: %v", err)
	}

	written, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(written) != stdout {
		t.Errorf("file content differs from stdout content\nfile:   %q\nstdout: %q", written, stdout)
	}
}

// TestDocsAgentsRejectsExtraArgs proves "docs agents somejunk" is a usage
// error (exit 2), not a silently-ignored positional. docsAgentsCmd sets no
// Args of its own, so this only holds because attachSubcommandGuards (see
// cmd/errors.go) stamps cobra.NoArgs onto every runnable leaf that left Args
// nil; this test exercises that path end to end, the same way Run() does.
func TestDocsAgentsRejectsExtraArgs(t *testing.T) {
	attachSubcommandGuards(rootCmd)

	if docsAgentsCmd.Args == nil {
		t.Fatalf("docsAgentsCmd.Args is nil; attachSubcommandGuards should have stamped cobra.NoArgs")
	}
	err := docsAgentsCmd.Args(docsAgentsCmd, []string{"somejunk"})
	if err == nil {
		t.Fatalf(`expected an error for "docs agents somejunk", got none`)
	}
	if got := exitCode(err); got != exitUsage {
		t.Errorf("exitCode(%v) = %d, want %d (exitUsage)", err, got, exitUsage)
	}
}

// TestDocsAgentsSkillAliasMatchesAgents proves "docs skill" (kept as an
// alias for backward compatibility) emits output identical to "docs agents".
func TestDocsAgentsSkillAliasMatchesAgents(t *testing.T) {
	agentsOut, _ := runDocsAgents(t, nil)

	found := false
	for _, alias := range docsAgentsCmd.Aliases {
		if alias == "skill" {
			found = true
		}
	}
	if !found {
		t.Fatalf(`docsAgentsCmd.Aliases does not contain "skill"`)
	}

	// The alias dispatches to the same *cobra.Command / RunE, so driving
	// RunE directly (as runDocsAgents does) already covers the alias's
	// behavior — this test's job is to assert the alias is actually wired up
	// and that Find() resolves "docs skill" to this same command.
	leaf, _, err := rootCmd.Find([]string{"docs", "skill"})
	if err != nil {
		t.Fatalf(`rootCmd.Find(["docs", "skill"]) failed: %v`, err)
	}
	if leaf != docsAgentsCmd {
		t.Fatalf(`"docs skill" resolved to %q, want docsAgentsCmd`, leaf.CommandPath())
	}

	skillOut, _ := runDocsAgents(t, nil)
	if skillOut != agentsOut {
		t.Errorf("docs skill output differs from docs agents output")
	}
}
