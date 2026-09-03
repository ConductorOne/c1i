package cmd

import (
	"bytes"
	"context"
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

// TestDocsAgentsHeaderVersionNotDoubled proves the rendered header carries
// the version exactly once. Version (from debug.ReadBuildInfo) already
// carries a leading "v" (e.g. "v0.4.1"), so a template of "v{{VERSION}}"
// renders "vv0.4.1".
func TestDocsAgentsHeaderVersionNotDoubled(t *testing.T) {
	orig := Version
	Version = "v9.9.9"
	t.Cleanup(func() { Version = orig })

	stdout, _ := runDocsAgents(t, nil)
	if strings.Contains(stdout, "vv9.9.9") {
		t.Errorf("rendered header doubles the version prefix:\n%s", stdout)
	}
	if !strings.Contains(stdout, "v9.9.9") {
		t.Errorf("rendered header missing version %q:\n%s", "v9.9.9", stdout)
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

	leaf, _, err := rootCmd.Find([]string{"docs", "skill"})
	if err != nil {
		t.Fatalf(`rootCmd.Find(["docs", "skill"]) failed: %v`, err)
	}
	if leaf != docsAgentsCmd {
		t.Fatalf(`"docs skill" resolved to %q, want docsAgentsCmd`, leaf.CommandPath())
	}

	// Drive the real tree under BOTH spellings. Comparing two calls to the
	// same helper would compare a function to itself and could never fail.
	skillOut := runThroughRoot(t, "docs", "skill")
	if skillOut != agentsOut {
		t.Errorf("\"docs skill\" output differs from \"docs agents\" output")
	}
	if viaRoot := runThroughRoot(t, "docs", "agents"); viaRoot != agentsOut {
		t.Errorf("\"docs agents\" via rootCmd differs from the direct RunE output")
	}
}

// runThroughRoot executes args against the real rootCmd and returns stdout,
// so a test can exercise a command by the name a user actually types.
func runThroughRoot(t *testing.T, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	// runDocsAgents sets writers directly on the package-level
	// docsAgentsCmd, and cobra prefers a command's own writer over its
	// parent's — so clear the leaf's writers or output lands in that stale
	// buffer instead of this one.
	docsAgentsCmd.SetOut(nil)
	docsAgentsCmd.SetErr(nil)
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})
	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("%v: unexpected error: %v", args, err)
	}
	return out.String()
}

// TestAgentsDocOpensWithFrontMatter pins that agents.md starts with its YAML
// block. `docs agents`'s own help promises the output opens with front matter
// that harnesses parse, and front matter is only front matter on line 1 — a
// bullet prepended above it silently demotes name/description/version to prose.
// Nothing else in the tree checks this, and the whole suite stayed green when
// it happened.
func TestAgentsDocOpensWithFrontMatter(t *testing.T) {
	if !strings.HasPrefix(agentsTemplate, "---\n") {
		first, _, _ := strings.Cut(agentsTemplate, "\n")
		t.Fatalf("agents.md must open with the YAML front-matter delimiter; it starts with %q", first)
	}
	// The block must close before the body starts. Scanning for the next "---"
	// anywhere would latch onto a thematic break if the real delimiter were
	// dropped, and report a body-sized block as valid front matter.
	rest := strings.TrimPrefix(agentsTemplate, "---\n")
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		t.Fatal("agents.md opens a front-matter block that is never closed")
	}
	if strings.Contains(rest[:end], "\n\n") {
		t.Fatal("agents.md's front-matter block is not closed before the body begins")
	}
	for _, key := range []string{"name:", "description:", "version:", "required_bins:"} {
		if !strings.Contains(rest[:end], key) {
			t.Errorf("front matter is missing %q, which docs agents' help says harnesses parse", key)
		}
	}
}
