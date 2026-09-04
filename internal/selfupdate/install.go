package selfupdate

import (
	"os"
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
	if inContainer() {
		return Docker, "you are running the container image; re-pull it: docker pull public.ecr.aws/conductorone/c1i"
	}
	if isHomebrew(execPath) {
		return Homebrew, "c1i was installed with Homebrew; upgrade it there: brew upgrade c1i"
	}
	if isGoInstall(execPath) {
		return GoInstall, "c1i was installed with `go install`; upgrade it there: go install github.com/ConductorOne/c1i@latest"
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

// isGoInstall reports whether execPath is under the Go install target
// (GOBIN, else GOPATH/bin, else ~/go/bin).
func isGoInstall(execPath string) bool {
	dir := filepath.Dir(execPath)
	if gobin := os.Getenv("GOBIN"); gobin != "" && sameDir(dir, gobin) {
		return true
	}
	for _, gp := range goPaths() {
		if sameDir(dir, filepath.Join(gp, "bin")) {
			return true
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if sameDir(dir, filepath.Join(home, "go", "bin")) {
			return true
		}
	}
	return false
}

func goPaths() []string {
	if gp := os.Getenv("GOPATH"); gp != "" {
		return filepath.SplitList(gp)
	}
	return nil
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
