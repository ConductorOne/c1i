package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// A ~/.c1i.yaml that exists but cannot be read or parsed used to be ignored.
func TestBrokenConfigFileIsAUsageError(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, path string)
	}{
		{"invalid yaml", func(t *testing.T, p string) { writeFile(t, p, "url: [unterminated\n  : :\n") }},
		{"non-mapping", func(t *testing.T, p string) { writeFile(t, p, "- a\n- b\n") }},
		{"directory", func(t *testing.T, p string) {
			if err := os.Mkdir(p, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{"unreadable", func(t *testing.T, p string) {
			if os.Geteuid() == 0 {
				t.Skip("root reads mode 000 files")
			}
			writeFile(t, p, "url: https://x.example.invalid\n")
			if err := os.Chmod(p, 0); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		for _, withURL := range []bool{true, false} {
			name := tc.name + "/no --url"
			if withURL {
				name = tc.name + "/--url"
			}
			t.Run(name, func(t *testing.T) {
				clearEnvFlags(t)
				home := t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("USERPROFILE", home)
				path := filepath.Join(home, ".c1i.yaml")
				tc.setup(t, path)
				args := append([]string{}, envTestArgs...)
				args = append(args, "--dry-run")
				if withURL {
					args = append(args, "--url", mutTestURL)
				} else {
					t.Setenv("C1I_URL", "")
				}
				r := runMutationCLI(t, nil, 0, nil, args...)
				if r.exit != exitUsage {
					t.Fatalf("exit = %d (%v), want %d", r.exit, r.err, exitUsage)
				}
				if !strings.Contains(r.stderr, path) {
					t.Errorf("stderr does not name %s:\n%s", path, r.stderr)
				}
				if strings.Contains(r.stderr, "url is required") {
					t.Errorf("misleading url error:\n%s", r.stderr)
				}
			})
		}
	}
}

func TestMissingConfigFileIsFine(t *testing.T) {
	clearEnvFlags(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	args := append(append([]string{}, envTestArgs...), "--dry-run")
	if r := runMutationCLI(t, nil, 0, nil, args...); r.err != nil {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
}

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Completion is local-only: like a bad C1I_DRY_RUN, a broken config file must
// not break it.
func TestBrokenConfigFileDoesNotBreakCompletion(t *testing.T) {
	clearEnvFlags(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeFile(t, filepath.Join(home, ".c1i.yaml"), "- a\n")
	if err := validateGlobalSettings(&cobra.Command{Use: cobra.ShellCompRequestCmd}); err != nil {
		t.Fatalf("validateGlobalSettings on __complete: %v", err)
	}
}

// docs and version need no config: a broken ~/.c1i.yaml must not stop them.
func TestBrokenConfigFileDoesNotBreakDocsOrVersion(t *testing.T) {
	origR, origC := embeddedReadme, embeddedChangelog
	t.Cleanup(func() { embeddedReadme, embeddedChangelog = origR, origC })
	embeddedReadme, embeddedChangelog = "# readme\n", "# changelog\n"
	for _, args := range [][]string{{"docs", "readme"}, {"docs", "changelog"}, {"docs", "agents"}, {"version"}, {"help"}, {"help", "docs"}, {"help", "api"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			clearEnvFlags(t)
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			writeFile(t, filepath.Join(home, ".c1i.yaml"), "- a\n")
			if r := runCLI(t, nil, nil, args...); r.err != nil {
				t.Fatalf("exit %d: %v", r.exit, r.err)
			}
		})
	}
}

// upgrade reads dry_run from the config file, so a broken file must still stop
// it rather than silently turn a preview into an install.
func TestUpgradeStillUsesConfigFile(t *testing.T) {
	clearEnvFlags(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeFile(t, filepath.Join(home, ".c1i.yaml"), "- a\n")
	d := newFakeDist(t)
	useDist(t, d, nil, "0.0.1")
	_, err := runUpgrade(t, "--dry-run")
	if exitCode(err) != exitUsage {
		t.Fatalf("exit = %d (%v), want %d", exitCode(err), err, exitUsage)
	}
	if len(d.requests) != 0 {
		t.Errorf("upgrade fetched %v before failing", d.requests)
	}
}

// attachSubcommandGuards (called by several tests, and by Execute) must leave
// cobra's lazily-created help command alone: NoArgs on it breaks `help <cmd>`.
func TestAttachSubcommandGuardsLeavesHelpCommandAlone(t *testing.T) {
	rootCmd.InitDefaultHelpCmd()
	attachSubcommandGuards(rootCmd)
	help, _, err := rootCmd.Find([]string{"help"})
	if err != nil || help.Name() != "help" {
		t.Fatalf("no help command: %v", err)
	}
	if help.Args != nil {
		t.Error("help command got an Args stamp; `help <cmd>` would be rejected")
	}
	clearEnvFlags(t)
	if r := runCLI(t, nil, nil, "help", "docs"); r.err != nil {
		t.Fatalf("help docs: exit %d: %v", r.exit, r.err)
	}
}
