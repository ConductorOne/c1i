package cmd

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/selfupdate"
	"github.com/ConductorOne/c1i/internal/transport"
)

// fakeUpgradeDoer serves a canned index.json for any /index.json request, so
// the upgrade decision tree can be exercised without the network.
type fakeUpgradeDoer struct{ index string }

func (f fakeUpgradeDoer) Do(req *http.Request) (*transport.Response, error) {
	if strings.HasSuffix(req.URL.Path, "/index.json") {
		return &transport.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       []byte(f.index),
		}, nil
	}
	return &transport.Response{StatusCode: http.StatusNotFound}, nil
}

func withUpgradeIndex(t *testing.T, index string) {
	t.Helper()
	orig := newUpgradeDoer
	newUpgradeDoer = func() selfupdate.Doer { return fakeUpgradeDoer{index: index} }
	t.Cleanup(func() { newUpgradeDoer = orig })
}

func withVersion(t *testing.T, v string) {
	t.Helper()
	orig := Version
	Version = v
	t.Cleanup(func() { Version = orig })
}

func runUpgrade(t *testing.T, args ...string) (string, error) {
	t.Helper()
	resetCmds(t, upgradeCmd)
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil) })
	rootCmd.SetArgs(append([]string{"upgrade"}, args...))
	err := rootCmd.ExecuteContext(t.Context())
	return out.String(), err
}

const idxStable06Latest07 = `{"channels":{"stable":"v0.6.0","latest":"v0.7.0"},"semvers":{"v0.6.0":{"yanked":false,"manifest":"m"},"v0.7.0":{"yanked":false,"manifest":"m"}}}`

func TestUpgradeAlreadyLatest(t *testing.T) {
	withUpgradeIndex(t, idxStable06Latest07)
	withVersion(t, "v0.6.0")
	out, err := runUpgrade(t)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "already the latest stable") {
		t.Errorf("output = %q", out)
	}
}

func TestUpgradeNewerThanChannel(t *testing.T) {
	withUpgradeIndex(t, idxStable06Latest07)
	withVersion(t, "v0.7.0") // ahead of stable (v0.6.0)
	out, err := runUpgrade(t)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "newer than the stable channel") {
		t.Errorf("output = %q", out)
	}
}

func TestUpgradeCheckReportsAvailable(t *testing.T) {
	withUpgradeIndex(t, idxStable06Latest07)
	withVersion(t, "v0.5.0") // behind stable
	out, err := runUpgrade(t, "--check")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "newer stable release is available: v0.5.0 -> v0.6.0") {
		t.Errorf("output = %q", out)
	}
}

func TestUpgradeChannelLatest(t *testing.T) {
	withUpgradeIndex(t, idxStable06Latest07)
	withVersion(t, "v0.6.0")
	out, err := runUpgrade(t, "--check", "--channel", "latest")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "available: v0.6.0 -> v0.7.0") {
		t.Errorf("output = %q", out)
	}
}

func TestUpgradeUnknownChannelIsUsageError(t *testing.T) {
	withUpgradeIndex(t, idxStable06Latest07)
	withVersion(t, "v0.6.0")
	_, err := runUpgrade(t, "--channel", "nightly")
	if err == nil {
		t.Fatal("expected an error for an unknown channel")
	}
	if got := exitCode(err); got != exitUsage {
		t.Errorf("exit = %d, want %d (usage)", got, exitUsage)
	}
}

func TestUpgradeYankedTargetErrors(t *testing.T) {
	withUpgradeIndex(t, `{"channels":{"stable":"v0.6.0"},"semvers":{"v0.6.0":{"yanked":true,"manifest":"m"}}}`)
	withVersion(t, "v0.5.0")
	_, err := runUpgrade(t)
	if err == nil {
		t.Fatal("expected an error when the channel points at a yanked version")
	}
	if got := exitCode(err); got != exitUpstream {
		t.Errorf("exit = %d, want %d (upstream)", got, exitUpstream)
	}
}

func TestUpgradeDevBuildDoesNotAttempt(t *testing.T) {
	withUpgradeIndex(t, idxStable06Latest07)
	withVersion(t, "dev")
	out, err := runUpgrade(t)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "development build") {
		t.Errorf("output = %q", out)
	}
}
