package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// captureStderr swaps os.Stderr for the duration of fn and returns everything
// written to it. warnAboutURLSource writes straight to os.Stderr (matching
// warnAboutURL's existing convention), not cmd.ErrOrStderr(), so a
// cmd.SetErr(&buf) capture — used elsewhere in this package — would not see
// it; this is the real, if cruder, file-descriptor swap needed to observe it.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = orig })

	fn()

	_ = w.Close()
	os.Stderr = orig

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()
	return buf.String()
}

// --- warnAboutURLSource unit tests ---

func TestWarnAboutURLSourceFlagIsSilent(t *testing.T) {
	out := captureStderr(t, func() {
		warnAboutURLSource("https://acme.conductor.one", URLSourceFlag)
	})
	if out != "" {
		t.Errorf("warnAboutURLSource with URLSourceFlag wrote %q to stderr, want nothing (an explicit --url is unambiguous)", out)
	}
}

// TestWarnAboutURLSourceEnvIsSilent is as load-bearing as the config-warns
// case: C1I_URL is exactly the source lost between shell calls in the
// incident this feature addresses, but the harmful step was the later
// fall-through to the config file, not the earlier (correct) C1I_URL calls.
// Warning here too would fire on every normal invocation of a legitimate
// workflow and train people to stop reading it -- guard against someone
// "helpfully" widening the rule back to include this source.
func TestWarnAboutURLSourceEnvIsSilent(t *testing.T) {
	out := captureStderr(t, func() {
		warnAboutURLSource("https://acme.conductor.one", URLSourceEnv)
	})
	if out != "" {
		t.Errorf("warnAboutURLSource with URLSourceEnv wrote %q to stderr, want nothing (C1I_URL is explicit, just invisible on the command line)", out)
	}
}

func TestWarnAboutURLSourceConfigWarns(t *testing.T) {
	out := captureStderr(t, func() {
		warnAboutURLSource("https://acme.conductor.one", URLSourceConfig)
	})
	if !strings.Contains(out, "https://acme.conductor.one") {
		t.Errorf("warning = %q, want it to name the resolved tenant URL", out)
	}
	if !strings.Contains(out, "~/.c1i.yaml") {
		t.Errorf("warning = %q, want it to name ~/.c1i.yaml", out)
	}
}

// --- end-to-end through GetBaseURLWithSource / a real command ---

// TestGetBaseURLWithSourceSilentForExplicitFlag is the regression guard: an
// explicit --url must never produce this warning, in unit form (not just via
// warnAboutURLSource directly) so a future refactor of GetBaseURLWithSource
// can't reintroduce it by skipping the source check.
func TestGetBaseURLWithSourceSilentForExplicitFlag(t *testing.T) {
	resetRootURLFlag(t)
	t.Setenv("C1I_URL", "")

	var stdout bytes.Buffer
	stderr := captureStderr(t, func() {
		rootCmd.SetOut(&stdout)
		rootCmd.SetErr(&stdout)
		rootCmd.SetArgs([]string{"users", "get", "someid", "--url", "acme.conductor.one"})
		_ = rootCmd.ExecuteContext(t.Context())
	})

	if strings.Contains(stderr, "Warning: no --url flag given") {
		t.Errorf("stderr = %q, an explicit --url must not trigger the tenant-source warning", stderr)
	}
}

// TestGetBaseURLWithSourceSilentForEnvSource is the end-to-end twin of
// TestWarnAboutURLSourceEnvIsSilent: C1I_URL alone must not produce the
// tenant-source warning through a real command invocation either, not just
// via warnAboutURLSource directly.
func TestGetBaseURLWithSourceSilentForEnvSource(t *testing.T) {
	resetRootURLFlag(t)
	t.Setenv("C1I_URL", "acme.conductor.one")

	var stdout bytes.Buffer
	stderr := captureStderr(t, func() {
		rootCmd.SetOut(&stdout)
		rootCmd.SetErr(&stdout)
		rootCmd.SetArgs([]string{"users", "get", "someid"})
		_ = rootCmd.ExecuteContext(t.Context())
	})

	if strings.Contains(stderr, "Warning: no --url flag given") {
		t.Errorf("stderr = %q, C1I_URL alone must not trigger the tenant-source warning", stderr)
	}
	if strings.Contains(stdout.String(), "Warning") {
		t.Errorf("stdout = %q, the tenant-source warning must never reach stdout", stdout.String())
	}
}

// TestGetBaseURLWithSourceWarnsForConfigSource proves the one source that
// does warn, driven the same way TestGetBaseURLBareTokenFromConfigNamesConfigFile
// drives it: via viper.Set rather than a real ~/.c1i.yaml (see that test's
// comment on why). The live verification separately drives an actual
// temporary config file end to end.
func TestGetBaseURLWithSourceWarnsForConfigSource(t *testing.T) {
	resetRootURLFlag(t)
	t.Setenv("C1I_URL", "")
	orig := viper.GetString("url")
	viper.Set("url", "acme.conductor.one")
	t.Cleanup(func() { viper.Set("url", orig) })

	var stdout bytes.Buffer
	stderr := captureStderr(t, func() {
		rootCmd.SetOut(&stdout)
		rootCmd.SetErr(&stdout)
		rootCmd.SetArgs([]string{"users", "get", "someid"})
		_ = rootCmd.ExecuteContext(t.Context())
	})

	if !strings.Contains(stderr, "Warning: no --url flag given") {
		t.Errorf("stderr = %q, want the tenant-source warning", stderr)
	}
	if !strings.Contains(stderr, "~/.c1i.yaml") {
		t.Errorf("stderr = %q, want it to name ~/.c1i.yaml", stderr)
	}
	if strings.Contains(stdout.String(), "Warning") {
		t.Errorf("stdout = %q, the tenant-source warning must never reach stdout", stdout.String())
	}
}

// TestGetBaseURLWithSourceWarnsOnceForPaginatedList proves the "once per
// invocation, not once per request" requirement: apps_list.go calls
// GetBaseURL a single time before its pagination loop, so a multi-page list
// must still print the warning exactly once even though it issues several
// HTTP requests. A real paginated fetch would need a live server, so this
// drives the same single-call code path apps_list.go relies on and asserts
// on invocation count instead.
func TestGetBaseURLWithSourceWarnsOnceForPaginatedList(t *testing.T) {
	resetRootURLFlag(t)
	t.Setenv("C1I_URL", "")
	orig := viper.GetString("url")
	viper.Set("url", "acme.conductor.one")
	t.Cleanup(func() { viper.Set("url", orig) })

	var stdout bytes.Buffer
	stderr := captureStderr(t, func() {
		rootCmd.SetOut(&stdout)
		rootCmd.SetErr(&stdout)
		rootCmd.SetArgs([]string{"apps", "list"})
		_ = rootCmd.ExecuteContext(t.Context())
	})

	if got := strings.Count(stderr, "Warning: no --url flag given"); got != 1 {
		t.Errorf("tenant-source warning printed %d times, want exactly 1 (apps_list.go calls GetBaseURL once, before its pagination loop)", got)
	}
}
