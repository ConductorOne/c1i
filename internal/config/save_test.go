package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// tempHome points the user's home directory at a temp dir and returns the
// config file path inside it.
func tempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return filepath.Join(home, ".c1i.yaml")
}

func readConfig(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path) // #nosec G304 -- a test temp file
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		t.Fatalf("saved config is not YAML: %v\n%s", err, b)
	}
	return m
}

func TestSaveToConfigFileCreatesFile(t *testing.T) {
	path := tempHome(t)
	if err := SaveToConfigFile("url", "https://example.conductor.one"); err != nil {
		t.Fatalf("SaveToConfigFile: %v", err)
	}
	if got := readConfig(t, path); len(got) != 1 || got["url"] != "https://example.conductor.one" {
		t.Errorf("config = %v, want only the url", got)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("mode = %o, want 600", fi.Mode().Perm())
		}
	}
}

// Saving round-trips through a map, so a user's comments are lost.
func TestSaveToConfigFileKeepsOtherKeysDropsComments(t *testing.T) {
	path := tempHome(t)
	existing := "# my tenant\nurl: https://old.example.conductor.one\nfields: id\n"
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveToConfigFile("url", "https://new.example.conductor.one"); err != nil {
		t.Fatalf("SaveToConfigFile: %v", err)
	}
	got := readConfig(t, path)
	if len(got) != 2 || got["url"] != "https://new.example.conductor.one" || got["fields"] != "id" {
		t.Errorf("config = %v, want the new url and fields: id", got)
	}
	b, _ := os.ReadFile(path) // #nosec G304 -- a test temp file
	if strings.Contains(string(b), "my tenant") {
		t.Errorf("comment survived; update this test's name and comment:\n%s", b)
	}
}

func TestSaveToConfigFileRefusesInvalidConfig(t *testing.T) {
	for _, bad := range []string{"url: [unclosed\nfields: id\n", "hello\n", "- a\n"} {
		t.Run(bad, func(t *testing.T) {
			path := tempHome(t)
			if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
				t.Fatal(err)
			}
			err := SaveToConfigFile("url", "https://example.conductor.one")
			if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "valid c1i config") {
				t.Errorf("SaveToConfigFile = %v, want an invalid-config error naming %s", err, path)
			}
			if b, _ := os.ReadFile(path); string(b) != bad { // #nosec G304 -- a test temp file
				t.Errorf("file = %q, want it unchanged", b)
			}
		})
	}
}

// A file with no settings in it is an empty config, not an invalid one.
func TestSaveToConfigFileAcceptsEmptyDocument(t *testing.T) {
	for _, empty := range []string{"", "  \n   \n", "# just a comment\n", "---\n", "~\n", "null\n"} {
		t.Run(empty, func(t *testing.T) {
			path := tempHome(t)
			if err := os.WriteFile(path, []byte(empty), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := SaveToConfigFile("url", "https://example.conductor.one"); err != nil {
				t.Fatalf("SaveToConfigFile: %v", err)
			}
			if got := readConfig(t, path); len(got) != 1 || got["url"] != "https://example.conductor.one" {
				t.Errorf("config = %v, want only the url", got)
			}
		})
	}
}

func TestSaveToConfigFileReadErrorWritesNothing(t *testing.T) {
	path := tempHome(t)
	// A directory in the file's place: it exists but can't be read as a file.
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := SaveToConfigFile("url", "https://example.conductor.one"); err == nil {
		t.Error("SaveToConfigFile = nil, want the read error")
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("home holds %d entries, want only the unreadable config", len(entries))
	}
}

// A write-only file can't be read but could be overwritten: it must not be.
func TestSaveToConfigFileUnreadableFileLeftAlone(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions enforced")
	}
	path := tempHome(t)
	if err := os.WriteFile(path, []byte("fields: id\n"), 0o200); err != nil {
		t.Fatal(err)
	}
	if err := SaveToConfigFile("url", "https://example.conductor.one"); err == nil {
		t.Error("SaveToConfigFile = nil, want the read error")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "fields: id\n" { // #nosec G304 -- a test temp file
		t.Errorf("file = %q, want it unchanged", b)
	}
}

func TestSaveToConfigFileRefusesRelativeHome(t *testing.T) {
	t.Setenv("HOME", ".")
	t.Setenv("USERPROFILE", ".")
	wd := t.TempDir()
	t.Chdir(wd)
	if err := SaveToConfigFile("url", "https://example.conductor.one"); err == nil {
		t.Error("SaveToConfigFile = nil, want an error for a relative home dir")
	}
	if entries, _ := os.ReadDir(wd); len(entries) != 0 {
		t.Errorf("wrote %v under the working directory", entries)
	}
}
