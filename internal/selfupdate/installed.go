package selfupdate

import (
	"debug/buildinfo"
	"fmt"
)

// InstalledVersion reads the module version embedded in an installed c1i binary.
func InstalledVersion(execPath string) (string, error) {
	info, err := buildinfo.ReadFile(execPath)
	if err != nil {
		return "", fmt.Errorf("reading build information for %s: %w", execPath, err)
	}
	if info.Main.Version == "" || info.Main.Version == "(devel)" {
		return "", fmt.Errorf("installed binary %s has no release version", execPath)
	}
	return info.Main.Version, nil
}
