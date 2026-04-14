package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// SaveToConfigFile reads ~/.c1i.yaml, sets the given key to value, and writes it back.
// The file is created with mode 0600 if it does not exist.
func SaveToConfigFile(key, value string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	path := filepath.Join(home, ".c1i.yaml")

	data := make(map[string]any)
	if existing, err := os.ReadFile(path); err == nil {
		_ = yaml.Unmarshal(existing, &data)
	}

	data[key] = value

	out, err := yaml.Marshal(data)
	if err != nil {
		return err
	}

	return os.WriteFile(path, out, 0600)
}
