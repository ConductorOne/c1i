package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/testenv"
)

func TestTestBinaryIsIsolatedFromRealEnvironment(t *testing.T) {
	if !testenv.Active() {
		t.Fatal("testenv.Isolate did not run in this test binary")
	}
	tmp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, fn := range map[string]func() (string, error){
		"home": os.UserHomeDir, "config": os.UserConfigDir, "cache": os.UserCacheDir,
	} {
		dir, err := fn()
		if err != nil {
			t.Fatalf("%s dir: %v", name, err)
		}
		dir = evalExistingPrefix(dir)
		if !strings.HasPrefix(dir, tmp+string(filepath.Separator)) {
			t.Errorf("%s dir %q is outside %q: tests can reach the real environment", name, dir, tmp)
		}
	}
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(strings.ToUpper(name), "C1I_") {
			t.Errorf("%s inherited into the test environment", name)
		}
	}
}

// evalExistingPrefix resolves symlinks in the longest existing prefix of p.
// darwin's config and cache dirs (~/Library/...) don't exist under a fresh
// HOME, and EvalSymlinks fails on a missing path, which would leave the
// /var -> /private/var link unresolved and the prefix check failing.
func evalExistingPrefix(p string) string {
	rest := ""
	for dir := p; ; dir = filepath.Dir(dir) {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return p
		}
		rest = filepath.Join(filepath.Base(dir), rest)
	}
}
