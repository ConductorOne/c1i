package selfupdate

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// Detect classifies how this binary was installed. execPath should be the
// resolved (symlink-followed) path of the running executable; goos is
// runtime.GOOS (a parameter so tests can exercise every branch). It returns
// the method and, for the non-self-replace methods, a one-line remediation the
// caller can print.
func Detect(execPath, goos string) (Method, string) {
	if goos == "windows" {
		return Windows, "download the Windows build (or MSI) from " + DefaultBaseURL
	}
	if goos == "linux" && inContainer() {
		return Docker, "you are running the container image; re-pull it: docker pull public.ecr.aws/conductorone/c1i"
	}
	if isHomebrew(execPath) {
		return Homebrew, "c1i was installed with Homebrew; upgrade it there: brew upgrade c1i"
	}
	if isGoInstall(execPath) {
		return GoInstall, "c1i was installed with `go install`; upgrade it there: go install github.com/ConductorOne/c1i@latest"
	}
	if isSystemInstall(execPath) {
		return SystemInstall, "c1i is installed in a system package directory; upgrade it with your system package manager"
	}
	return Standalone, ""
}

// containerMarkerFiles are the runtime-dropped marker files whose presence
// signals a container. Overridable so a test can point them at a temp file.
// /.dockerenv is Docker's; /run/.containerenv is Podman's.
var containerMarkerFiles = []string{"/.dockerenv", "/run/.containerenv"}

// containerCgroupFile is the cgroup path inspected for runtime markers.
// Overridable for the same reason.
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

var readGoEnv = func() (goEnv, error) {
	output, err := exec.Command("go", "env", "-json", "GOBIN", "GOPATH").Output()
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

// SelfReplaceGOOS reports whether the current OS supports replacing the running
// binary in place (POSIX rename over a running executable). Windows does not.
func SelfReplaceGOOS() bool { return runtime.GOOS != "windows" }
