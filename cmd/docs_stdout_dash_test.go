package cmd

import (
	"os"
	"strings"
	"testing"
)

// `-o -` is stdout, not a file named "-".
func TestDocsOutputDashIsStdout(t *testing.T) {
	for _, name := range []string{"readme", "changelog", "agents"} {
		t.Run(name, func(t *testing.T) {
			origR, origC := embeddedReadme, embeddedChangelog
			t.Cleanup(func() { embeddedReadme, embeddedChangelog = origR, origC })
			embeddedReadme = "# readme\n" + strings.Repeat("text ", 40)
			embeddedChangelog = "# changelog\n" + strings.Repeat("text ", 40)
			t.Chdir(t.TempDir())
			t.Setenv("C1I_URL", mutTestURL)
			r := runCLI(t, nil, nil, "docs", name, "-o", "-")
			if r.err != nil {
				t.Fatalf("exit %d: %v", r.exit, r.err)
			}
			if _, err := os.Stat("-"); err == nil {
				t.Error(`wrote a file named "-"`)
			}
			if len(strings.TrimSpace(r.stdout)) < 100 {
				t.Errorf("stdout has no document (%d bytes)", len(r.stdout))
			}
			if strings.Contains(r.stderr, "Wrote ") {
				t.Errorf("claimed to write a file: %s", r.stderr)
			}
		})
	}
}
