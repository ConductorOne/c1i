package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// HomeDir is os.UserHomeDir, refusing a relative result: with HOME="." the
// config file would be read from, and written to, the working directory.
func HomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("home dir %q is not an absolute path", home)
	}
	return home, nil
}

// SaveToConfigFile reads ~/.c1i.yaml, sets the given key to value, and writes it back.
// The file is created with mode 0600 if it does not exist. A file it can't read
// or parse is left untouched and reported, so the user's settings aren't lost.
func SaveToConfigFile(key, value string) error {
	home, err := HomeDir()
	if err != nil {
		return err
	}

	path := filepath.Join(home, ".c1i.yaml")

	data := make(map[string]any)
	existing, err := os.ReadFile(path) // #nosec G304 -- path is fixed (~/.c1i.yaml), not caller input
	switch {
	case err == nil:
		if err := yaml.Unmarshal(existing, &data); err != nil {
			return fmt.Errorf("%s isn't a valid c1i config (a YAML mapping), so it was left unchanged: %w", path, err)
		}
		if data == nil { // a document of only "---", "~" or "null"
			data = map[string]any{}
		}
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}

	data[key] = value

	out, err := yaml.Marshal(data)
	if err != nil {
		return err
	}

	return os.WriteFile(path, out, 0600)
}
