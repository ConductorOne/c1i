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

// An unparseable file is not an error: it is replaced by the one key.
func TestSaveToConfigFileReplacesUnparseableFile(t *testing.T) {
	path := tempHome(t)
	if err := os.WriteFile(path, []byte("url: [unclosed\nfields: id\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveToConfigFile("url", "https://example.conductor.one"); err != nil {
		t.Fatalf("SaveToConfigFile = %v, want nil", err)
	}
	if got := readConfig(t, path); len(got) != 1 || got["url"] != "https://example.conductor.one" {
		t.Errorf("config = %v, want only the new url", got)
	}
}
