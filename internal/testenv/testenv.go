// Package testenv isolates a test binary from the developer's real home,
// config and C1I_* settings. Import it only from _test.go files.
package testenv

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/ConductorOne/c1i/internal/timing"
)

// DeadURL refuses connections. Port 1 is never handed out from the ephemeral
// range and no test binds it; a closed ephemeral port can be reused by a
// concurrent `go test` process and answer a client that is still retrying.
const DeadURL = "http://127.0.0.1:1"

var active bool

// Active reports whether Isolate ran in this process.
func Active() bool { return active }

// Isolate points every home/config/cache variable that os.UserHomeDir,
// os.UserConfigDir and os.UserCacheDir consult at a fresh temp dir and unsets
// all C1I_* variables. It panics on failure; call the result when done.
func Isolate() (cleanup func()) {
	pinGoDirs()
	dir, err := os.MkdirTemp("", "c1i-testenv-")
	if err != nil {
		panic(fmt.Sprintf("testenv: %v", err))
	}
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(strings.ToUpper(name), "C1I_") {
			_ = os.Unsetenv(name)
		}
	}
	for _, name := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "APPDATA", "LOCALAPPDATA"} {
		if err := os.Setenv(name, dir); err != nil {
			panic(fmt.Sprintf("testenv: %v", err))
		}
	}
	active = true
	return func() { active = false; _ = os.RemoveAll(dir) }
}

// pinGoDirs freezes the go tool's own resolution of its env file and caches
// (which honors `go env -w`) so `go` children keep their settings and warm
// cache after HOME moves. Best effort: without a go binary there are no children.
func pinGoDirs() {
	out, err := exec.Command("go", "env", "-json", "GOENV", "GOCACHE", "GOMODCACHE", "GOPATH").Output()
	if err != nil {
		return
	}
	var env map[string]string
	if json.Unmarshal(out, &env) != nil {
		return
	}
	for k, v := range env {
		if v != "" && os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
}

// WaitDivisor shrinks login's poll interval and the retry backoff in tests
// (500ms becomes 0.5ms); tests assert counts and computed delays, not seconds.
const WaitDivisor = 1000

// Run is a TestMain body: isolate, swap in go-keyring's in-memory mock (global
// and irreversible, so it is set once here), run the tests, clean up. Use as
// os.Exit(testenv.Run(m)).
func Run(m *testing.M) int {
	cleanup := Isolate()
	defer cleanup()
	keyring.MockInit()
	timing.Divisor = WaitDivisor
	return m.Run()
}
