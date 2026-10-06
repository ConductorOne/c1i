package selfupdate

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Method is how the running c1i binary was installed. It decides whether a
// self-replace is appropriate or the user should upgrade through their package
// manager.
type Method int

const (
	// Standalone is a plain downloaded binary — safe to replace in place.
	Standalone Method = iota
	// Homebrew binaries are symlinks into a Cellar; `brew upgrade` owns them.
	Homebrew
	// GoInstall binaries live in GOBIN/GOPATH/bin; `go install ...@latest` owns them.
	GoInstall
	// Docker means we are inside a container image — the image is replaced by re-pulling, not in place.
	Docker
	// SystemInstall is owned by a system package manager, not this updater.
	SystemInstall
	// Windows is handled separately: a running .exe cannot be overwritten in place, and the channel is an MSI.
	Windows
)

// String names the method for machine-readable output.
func (m Method) String() string {
	switch m {
	case Homebrew:
		return "homebrew"
	case GoInstall:
		return "go-install"
	case Docker:
		return "container"
	case SystemInstall:
		return "system"
	case Windows:
		return "windows"
	default:
		return "standalone"
	}
}

// Command is the command that upgrades an install of this kind, or "" when
// there is none to run (a standalone binary upgrades itself).
func (m Method) Command() string {
	switch m {
	case Homebrew:
		return "brew upgrade c1i"
	case GoInstall:
		return "go install github.com/ConductorOne/c1i@latest"
	case Docker:
		return "docker pull public.ecr.aws/conductorone/c1i"
	default:
		return ""
	}
}

// Detect classifies how the binary at the resolved execPath was installed and,
// unless it is Standalone, returns a one-line remediation to print.
func Detect(execPath, goos string) (Method, string) {
	if goos == "windows" {
		return Windows, "download the Windows build (or MSI) from " + DefaultBaseURL
	}
	if goos == "linux" && inContainer() {
		return Docker, "you are running the container image; re-pull it: " + Docker.Command()
	}
	if isHomebrew(execPath) {
		return Homebrew, "c1i was installed with Homebrew; upgrade it there: " + Homebrew.Command()
	}
	if isGoInstall(execPath) {
		return GoInstall, "c1i was installed with `go install`; upgrade it there: " + GoInstall.Command()
	}
	if isSystemInstall(execPath) {
		return SystemInstall, "c1i is installed in a system package directory; upgrade it with your system package manager"
	}
	return Standalone, ""
}

// Docker's and Podman's container markers; vars so tests can redirect them.
var containerMarkerFiles = []string{"/.dockerenv", "/run/.containerenv"}

var containerCgroupFile = "/proc/1/cgroup"

// inContainer reports whether we are running inside a container image, where an
// in-place replace is pointless (the layer is ephemeral). Best-effort and
// Linux-shaped; false on macOS.
func inContainer() bool {
	for _, marker := range containerMarkerFiles {
		if _, err := os.Stat(marker); err == nil {
			return true
		}
	}
	// cgroup v1 names the controller path; container runtimes leave a marker.
	if b, err := os.ReadFile(containerCgroupFile); err == nil {
		s := string(b)
		for _, marker := range []string{"docker", "containerd", "kubepods", "/lxc/"} {
			if strings.Contains(s, marker) {
				return true
			}
		}
	}
	return false
}

// isHomebrew reports whether execPath resolves into a Homebrew Cellar. Brew
// installs the binary in <prefix>/bin as a symlink to
// <prefix>/Cellar/<formula>/<version>/bin/<name>, so the resolved path
// contains "/Cellar/".
func isHomebrew(execPath string) bool {
	return strings.Contains(execPath, "/Cellar/")
}

func isSystemInstall(execPath string) bool {
	dir := filepath.Clean(filepath.Dir(execPath))
	return dir == "/bin" || dir == "/usr/bin"
}

// goEnv contains Go's effective persisted install settings.
type goEnv struct {
	GOBIN  string
	GOPATH string
}

// readGoEnv runs from a neutral directory with GOTOOLCHAIN=local, so a go.mod
// or go.work in the caller's cwd can't trigger a toolchain download.
var readGoEnv = func() (goEnv, error) {
	cmd := exec.Command("go", "env", "-json", "GOBIN", "GOPATH")
	cmd.Dir = os.TempDir()
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	output, err := cmd.Output()
	if err != nil {
		return goEnv{}, err
	}
	var env goEnv
	if err := json.Unmarshal(output, &env); err != nil {
		return goEnv{}, err
	}
	return env, nil
}

// isGoInstall reports whether execPath is under any effective Go install target.
func isGoInstall(execPath string) bool {
	dir := filepath.Dir(execPath)
	for _, target := range goInstallTargets() {
		if sameDir(dir, target) {
			return true
		}
	}
	return false
}

func goInstallTargets() []string {
	var targets []string
	addGoPaths := func(gopath string) {
		for _, gp := range filepath.SplitList(gopath) {
			if gp != "" {
				targets = append(targets, filepath.Join(gp, "bin"))
			}
		}
	}
	if gobin := os.Getenv("GOBIN"); gobin != "" {
		targets = append(targets, gobin)
	}
	addGoPaths(os.Getenv("GOPATH"))
	if persisted, err := readGoEnv(); err == nil {
		if persisted.GOBIN != "" {
			targets = append(targets, persisted.GOBIN)
		}
		addGoPaths(persisted.GOPATH)
	}
	if home, err := os.UserHomeDir(); err == nil {
		targets = append(targets, filepath.Join(home, "go", "bin"))
	}
	return targets
}

func sameDir(a, b string) bool {
	ac := filepath.Clean(a)
	bc := filepath.Clean(b)
	if ac == bc {
		return true
	}
	// Fall back to a resolved comparison so a symlinked GOPATH still matches.
	if ra, err := filepath.EvalSymlinks(ac); err == nil {
		if rb, err := filepath.EvalSymlinks(bc); err == nil {
			return ra == rb
		}
	}
	return false
}

// ExecutablePath returns the resolved path of the running binary.
func ExecutablePath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved, nil
	}
	return p, nil
}
