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
	for _, args := range [][]string{{"docs", "readme"}, {"docs", "changelog"}, {"docs", "agents"}, {"version"}} {
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
