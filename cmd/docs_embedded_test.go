package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func runEmbeddedDoc(t *testing.T, c *cobra.Command, args []string) (stdout, stderr string, err error) {
	t.Helper()
	outBuf, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	c.SetOut(outBuf)
	c.SetErr(errBuf)
	resetCmdFlags(t, c)
	if err := c.ParseFlags(args); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	err = c.RunE(c, c.Flags().Args())
	return outBuf.String(), errBuf.String(), err
}

func withEmbeddedDocs(t *testing.T, readme, changelog, version string) {
	t.Helper()
	origV := Version
	origR, origC := embeddedReadme, embeddedChangelog
	t.Cleanup(func() { Version, embeddedReadme, embeddedChangelog = origV, origR, origC })
	Version = version
	SetEmbeddedDocs(readme, changelog)
}

func TestDocsReadmeAndChangelogPrintAsIs(t *testing.T) {
	const readme = "# R\n\nlink [x](docs/y.md) {{VERSION}}\n"
	const changelog = "# C\n\n- entry\n"
	withEmbeddedDocs(t, readme, changelog, "v1.2.3")

	for _, tc := range []struct {
		cmd  *cobra.Command
		want string
		file string
	}{
		{docsReadmeCmd, readme, "README.md"},
		{docsChangelogCmd, changelog, "CHANGELOG.md"},
	} {
		stdout, stderr, err := runEmbeddedDoc(t, tc.cmd, nil)
		if err != nil {
			t.Fatalf("%s: %v", tc.file, err)
		}
		if stdout != tc.want {
			t.Errorf("%s stdout = %q, want as-is %q", tc.file, stdout, tc.want)
		}
		wantURL := "https://github.com/ConductorOne/c1i/blob/v1.2.3/" + tc.file
		if !strings.Contains(stderr, wantURL) {
			t.Errorf("%s stderr %q lacks pinned URL %q", tc.file, stderr, wantURL)
		}
	}
}

func TestDocsEmbeddedFooterDevBuildPointsAtMain(t *testing.T) {
	withEmbeddedDocs(t, "r", "c", "dev")
	_, stderr, err := runEmbeddedDoc(t, docsReadmeCmd, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "/blob/main/README.md") {
		t.Errorf("dev footer = %q, want main ref", stderr)
	}
}

func TestDocsEmbeddedOutputFlag(t *testing.T) {
	withEmbeddedDocs(t, "readme body\n", "changelog body\n", "v1.2.3")
	for _, tc := range []struct {
		cmd  *cobra.Command
		want string
	}{{docsReadmeCmd, "readme body\n"}, {docsChangelogCmd, "changelog body\n"}} {
		path := filepath.Join(t.TempDir(), "out.md")
		stdout, _, err := runEmbeddedDoc(t, tc.cmd, []string{"--output", path})
		if err != nil {
			t.Fatal(err)
		}
		if stdout != "" {
			t.Errorf("stdout = %q with --output, want empty", stdout)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != tc.want {
			t.Errorf("file = %q, %v; want %q", got, err, tc.want)
		}
	}
}

func TestDocsEmbeddedMissingContentIsAnError(t *testing.T) {
	withEmbeddedDocs(t, "", "", "v1.2.3")
	if _, _, err := runEmbeddedDoc(t, docsReadmeCmd, nil); err == nil {
		t.Error("empty embedded README should error, not print nothing and exit 0")
	}
}

func TestDocsReadmeHelpDistinguishesFromAgents(t *testing.T) {
	if !strings.Contains(docsReadmeCmd.Long, "full reference") || !strings.Contains(docsReadmeCmd.Long, "docs agents") {
		t.Errorf("docs readme --help must say README is the full reference and docs agents the concise agent guide:\n%s", docsReadmeCmd.Long)
	}
}
