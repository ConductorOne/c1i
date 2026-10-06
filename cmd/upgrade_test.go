package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/selfupdate"
	"github.com/ConductorOne/c1i/internal/transport"
	"github.com/sigstore/sigstore-go/pkg/root"
)

// fakeDist is an httptest distribution center. files maps a path under /c1i
// to its body; handler, when set, answers instead.
type fakeDist struct {
	srv      *httptest.Server
	files    map[string][]byte
	handler  http.HandlerFunc
	requests []string
}

func newFakeDist(t *testing.T) *fakeDist {
	t.Helper()
	d := &fakeDist{files: map[string][]byte{}}
	d.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.requests = append(d.requests, r.URL.Path)
		if d.handler != nil {
			d.handler(w, r)
			return
		}
		body, ok := d.files[strings.TrimPrefix(r.URL.Path, "/c1i/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".json") {
			w.Header().Set("Content-Type", "application/json")
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(d.srv.Close)
	return d
}

func (d *fakeDist) url(path string) string { return d.srv.URL + "/c1i/" + path }

func (d *fakeDist) requested(path string) bool {
	for _, p := range d.requests {
		if p == "/c1i/"+path {
			return true
		}
	}
	return false
}

// release publishes a release: an index whose stable channel points at
// target, a manifest for manifestSemver signed by sig, and the archive.
type release struct {
	target, manifestSemver string
	archive                []byte
	sha                    string // manifest's sha256; the archive's when empty
	unsigned               bool
	// tamper rewrites the manifest after it is signed.
	tamper func(m map[string]any)
}

func (d *fakeDist) publish(t *testing.T, sig *fakeSigstore, r release) {
	t.Helper()
	if r.sha == "" {
		sum := sha256.Sum256(r.archive)
		r.sha = hex.EncodeToString(sum[:])
	}
	d.files["c1i.tar.gz"] = r.archive
	manifest := map[string]any{
		"semver":              r.manifestSemver,
		"signatureBundleHref": d.url("manifest.json.sigstore.json"),
		"assets": map[string]any{selfupdate.PlatformKey(): map[string]any{
			"filename": "c1i.tar.gz", "sha256": r.sha, "href": d.url("c1i.tar.gz"),
		}},
	}
	raw, _ := json.Marshal(manifest)
	sigB64, certB64, bundle := sig.sign(t, raw, r.manifestSemver)
	if r.tamper != nil {
		r.tamper(manifest)
		raw, _ = json.Marshal(manifest)
	}
	d.files["manifest.json"] = raw
	d.files["manifest.json.sig"] = sigB64
	d.files["manifest.json.cert"] = certB64
	d.files["manifest.json.sigstore.json"] = bundle
	entry := map[string]any{"manifest": d.url("manifest.json"), "signature": d.url("manifest.json.sig"), "certificate": d.url("manifest.json.cert")}
	if r.unsigned {
		delete(entry, "signature")
		delete(entry, "certificate")
	}
	idx, _ := json.Marshal(map[string]any{
		"channels": map[string]any{"stable": r.target},
		"semvers":  map[string]any{r.target: entry},
	})
	d.files["index.json"] = idx
}

// useDist points upgrade at d, verifying against tr, and installs into a
// temp binary reporting installed. It returns the binary's path.
func useDist(t *testing.T, d *fakeDist, tr root.TrustedMaterial, installed string) string {
	t.Helper()
	origClient, origExe, origInstalled := newUpgradeClient, upgradeExecutable, installedVersion
	t.Cleanup(func() { newUpgradeClient, upgradeExecutable, installedVersion = origClient, origExe, origInstalled })
	newUpgradeClient = func() *selfupdate.Client {
		doer := transport.New(d.srv.Client().Transport, transport.WithMaxRetries(0))
		return &selfupdate.Client{HTTP: doer, BaseURL: d.srv.URL + "/c1i", TrustedRoot: func(context.Context) (root.TrustedMaterial, error) { return tr, nil }}
	}
	exe := filepath.Join(t.TempDir(), "c1i")
	if err := os.WriteFile(exe, []byte("OLD BINARY"), 0o755); err != nil {
		t.Fatal(err)
	}
	if m, _ := selfupdate.Detect(exe, runtime.GOOS); m != selfupdate.Standalone {
		t.Skipf("a temp binary is detected as %s on this host", m)
	}
	upgradeExecutable = func() (string, error) { return exe, nil }
	installedVersion = func(string) (string, error) { return installed, nil }
	withVersion(t, installed)
	return exe
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
	t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil); rootCmd.SetIn(nil) })
	upgradeCmd.SetContext(t.Context()) // cobra keeps a subcommand's first context
	rootCmd.SetArgs(append([]string{"upgrade"}, args...))
	err := rootCmd.ExecuteContext(t.Context())
	return out.String(), err
}

func tarGz(t *testing.T, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "c1i", Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	_, _ = tw.Write(content)
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestUpgradeReplacesBinaryWithVerifiedRelease(t *testing.T) {
	sig, d := newFakeSigstore(t), newFakeDist(t)
	d.publish(t, sig, release{target: "v0.7.0", manifestSemver: "v0.7.0", archive: tarGz(t, []byte("NEW BINARY"))})
	exe := useDist(t, d, sig.root, "v0.6.0")
	out, err := runUpgrade(t, "-y")
	if err != nil {
		t.Fatalf("upgrade: %v\n%s", err, out)
	}
	if got := readFile(t, exe); got != "NEW BINARY" {
		t.Errorf("binary = %q, want the release's", got)
	}
	if entries, _ := os.ReadDir(filepath.Dir(exe)); len(entries) != 1 {
		t.Errorf("install dir has %d entries, want only c1i", len(entries))
	}
}

// Each case must fail before the binary is touched.
func TestUpgradeRefusesUnverifiedRelease(t *testing.T) {
	good := []byte("NEW BINARY")
	evil := tarGz(t, []byte("EVIL BINARY"))
	evilSum := sha256.Sum256(evil)
	cases := []struct {
		name string
		rel  release
	}{
		{"unsigned", release{target: "v0.7.0", manifestSemver: "v0.7.0", archive: tarGz(t, good), unsigned: true}},
		{"manifest tampered after signing", release{target: "v0.7.0", manifestSemver: "v0.7.0", archive: evil,
			sha: strings.Repeat("0", 64),
			tamper: func(m map[string]any) {
				m["assets"].(map[string]any)[selfupdate.PlatformKey()].(map[string]any)["sha256"] = hex.EncodeToString(evilSum[:])
			}}},
		{"signed manifest for another version", release{target: "v0.7.0", manifestSemver: "v0.6.5", archive: evil}},
		{"archive does not match the manifest", release{target: "v0.7.0", manifestSemver: "v0.7.0", archive: evil,
			sha: hex.EncodeToString(sha256.New().Sum(nil))}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sig, d := newFakeSigstore(t), newFakeDist(t)
			d.publish(t, sig, tc.rel)
			exe := useDist(t, d, sig.root, "v0.6.0")
			out, err := runUpgrade(t, "-y")
			if got := exitCode(err); got != exitUpstream {
				t.Errorf("exit = %d (%v), want %d\n%s", got, err, exitUpstream, out)
			}
			if got := readFile(t, exe); got != "OLD BINARY" {
				t.Errorf("binary = %q, want it untouched", got)
			}
		})
	}
}

func TestUpgradeExitCodes(t *testing.T) {
	status := func(code int) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
	}
	cases := []struct {
		name    string
		handler http.HandlerFunc
		closed  bool
		want    int
		inMsg   string
	}{
		{"index not found", status(http.StatusNotFound), false, exitNotFound, "reading release channels"},
		{"rate limited", status(http.StatusTooManyRequests), false, exitRateLimited, "reading release channels"},
		{"server error", status(http.StatusBadGateway), false, exitServer, "reading release channels"},
		// dist needs no auth and takes no caller input: other 4xx are dist's fault.
		{"unauthorized", status(http.StatusUnauthorized), false, exitUpstream, "reading release channels"},
		{"forbidden", status(http.StatusForbidden), false, exitUpstream, "reading release channels"},
		{"bad request", status(http.StatusBadRequest), false, exitUpstream, "reading release channels"},
		{"redirect refused", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		}, false, exitUpstream, "reading release channels"},
		{"unreachable", nil, true, exitUpstream, "reading release channels"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newFakeDist(t)
			d.handler = tc.handler
			useDist(t, d, nil, "v0.6.0")
			if tc.closed {
				d.srv.Close()
			}
			_, err := runUpgrade(t, "-y")
			if got := exitCode(err); got != tc.want {
				t.Errorf("exit = %d (%v), want %d", got, err, tc.want)
			}
			if err == nil || !strings.Contains(displayError(err).Error(), tc.inMsg) {
				t.Errorf("error = %v, want it to mention %q", err, tc.inMsg)
			}
		})
	}
}

func TestUpgradeLocalFailuresExitOneAndNameTheDirectory(t *testing.T) {
	var d *fakeDist
	setup := func(t *testing.T) string {
		var sig *fakeSigstore
		sig, d = newFakeSigstore(t), newFakeDist(t)
		d.publish(t, sig, release{target: "v0.7.0", manifestSemver: "v0.7.0", archive: tarGz(t, []byte("NEW BINARY"))})
		return useDist(t, d, sig.root, "v0.6.0")
	}
	t.Run("install directory not writable", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root ignores directory permissions")
		}
		exe := setup(t)
		dir := filepath.Dir(exe)
		if err := os.Chmod(dir, 0o555); err != nil { // #nosec G302 -- a test dir
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) }) // #nosec G302
		_, err := runUpgrade(t, "-y")
		if got := exitCode(err); got != exitError {
			t.Errorf("exit = %d (%v), want %d", got, err, exitError)
		}
		if err == nil || strings.Count(err.Error(), dir) != 1 {
			t.Errorf("error = %v, want it to name %s once", err, dir)
		}
		if got := readFile(t, exe); got != "OLD BINARY" {
			t.Errorf("binary = %q, want it untouched", got)
		}
		if d.requested("c1i.tar.gz") {
			t.Error("downloaded the release before finding the install directory unwritable")
		}
	})
	t.Run("replace fails after download", func(t *testing.T) {
		exe := setup(t)
		// A directory where the binary should be: staging works, the rename can't.
		if err := os.Remove(exe); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(exe, "keep"), 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := runUpgrade(t, "-y")
		if got := exitCode(err); got != exitError {
			t.Errorf("exit = %d (%v), want %d", got, err, exitError)
		}
		if dir := filepath.Dir(exe); err == nil || strings.Count(err.Error(), dir) != 1 {
			t.Errorf("error = %v, want it to name %s once", err, dir)
		}
		if _, statErr := os.Stat(filepath.Join(exe, "keep")); statErr != nil {
			t.Errorf("install path changed: %v", statErr)
		}
	})
	t.Run("upgrade already running", func(t *testing.T) {
		exe := setup(t)
		unlock, err := selfupdate.LockExecutable(exe)
		if err != nil {
			t.Fatal(err)
		}
		defer unlock()
		_, err = runUpgrade(t, "-y")
		if got := exitCode(err); got != exitError {
			t.Errorf("exit = %d (%v), want %d", got, err, exitError)
		}
		if err == nil || !strings.Contains(err.Error(), filepath.Dir(exe)) {
			t.Errorf("error = %v, want it to name the install directory", err)
		}
	})
}

func TestUpgradeConfirmReadsCommandInput(t *testing.T) {
	for _, tc := range []struct {
		answer, want string
	}{{"y\n", "NEW BINARY"}, {"n\n", "OLD BINARY"}} {
		t.Run(strings.TrimSpace(tc.answer), func(t *testing.T) {
			sig, d := newFakeSigstore(t), newFakeDist(t)
			d.publish(t, sig, release{target: "v0.7.0", manifestSemver: "v0.7.0", archive: tarGz(t, []byte("NEW BINARY"))})
			exe := useDist(t, d, sig.root, "v0.6.0")
			origTerm := isTerminal
			isTerminal = func() bool { return true }
			t.Cleanup(func() { isTerminal = origTerm })
			rootCmd.SetIn(strings.NewReader(tc.answer))
			if out, err := runUpgrade(t); err != nil {
				t.Fatalf("upgrade: %v\n%s", err, out)
			}
			if got := readFile(t, exe); got != tc.want {
				t.Errorf("binary = %q, want %q", got, tc.want)
			}
		})
	}
}

func publishIndex(t *testing.T, d *fakeDist, index string) {
	t.Helper()
	d.files["index.json"] = []byte(index)
}

const idxStable06Latest07 = `{"channels":{"stable":"v0.6.0","latest":"v0.7.0"},"semvers":{"v0.6.0":{"manifest":"m"},"v0.7.0":{"manifest":"m"}}}`

func TestUpgradeCheckReportsJSON(t *testing.T) {
	cases := []struct {
		name, current, channel, exe string
		want                        map[string]any
	}{
		{"behind stable", "v0.5.0", "stable", "", map[string]any{
			"current": "v0.5.0", "latest": "v0.6.0", "channel": "stable", "update_available": true, "install_method": "standalone",
		}},
		{"current on stable", "v0.6.0", "stable", "", map[string]any{
			"current": "v0.6.0", "latest": "v0.6.0", "channel": "stable", "update_available": false, "install_method": "standalone",
		}},
		{"behind latest, Homebrew", "v0.6.0", "latest", "/opt/homebrew/Cellar/c1i/0.6.0/bin/c1i", map[string]any{
			"current": "v0.6.0", "latest": "v0.7.0", "channel": "latest", "update_available": true,
			"install_method": "homebrew", "upgrade_command": "brew upgrade c1i",
		}},
		{"pseudo-version build", "v0.6.1-0.20261005220836-bb1be01ebd69", "stable", "", map[string]any{
			"current": "v0.6.1-0.20261005220836-bb1be01ebd69", "latest": "v0.6.0", "channel": "stable",
			"update_available": false, "install_method": "standalone",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newFakeDist(t)
			publishIndex(t, d, idxStable06Latest07)
			useDist(t, d, nil, tc.current)
			if tc.exe != "" {
				upgradeExecutable = func() (string, error) { return tc.exe, nil }
			}
			out, err := runUpgrade(t, "--check", "--channel", tc.channel)
			if err != nil {
				t.Fatalf("upgrade --check: %v", err)
			}
			var got map[string]any
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatalf("output is not JSON: %v\n%s", err, out)
			}
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(tc.want)
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("report = %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestUpgradeNonStandalonePrintsCommand(t *testing.T) {
	d := newFakeDist(t)
	publishIndex(t, d, idxStable06Latest07)
	useDist(t, d, nil, "v0.5.0")
	upgradeExecutable = func() (string, error) { return "/opt/homebrew/Cellar/c1i/0.5.0/bin/c1i", nil }
	out, err := runUpgrade(t, "-y")
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if !strings.Contains(out, "brew upgrade c1i") {
		t.Errorf("output = %q, want the Homebrew command", out)
	}
	if len(d.requests) != 1 {
		t.Errorf("requests = %v, want only the index for an install it will not replace", d.requests)
	}
}

func TestUpgradeAlreadyCurrent(t *testing.T) {
	d := newFakeDist(t)
	publishIndex(t, d, idxStable06Latest07)
	useDist(t, d, nil, "v0.7.0")
	out, err := runUpgrade(t, "--channel", "latest")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "c1i v0.7.0 is current on the latest channel") {
		t.Errorf("output = %q", out)
	}
}

func TestUpgradeNewerThanChannel(t *testing.T) {
	d := newFakeDist(t)
	publishIndex(t, d, idxStable06Latest07)
	useDist(t, d, nil, "v0.7.0") // ahead of stable (v0.6.0)
	out, err := runUpgrade(t)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "newer than the stable channel") {
		t.Errorf("output = %q", out)
	}
}

func TestUpgradeUnknownChannelIsUsageError(t *testing.T) {
	d := newFakeDist(t)
	publishIndex(t, d, idxStable06Latest07)
	useDist(t, d, nil, "v0.6.0")
	_, err := runUpgrade(t, "--channel", "nightly")
	if got := exitCode(err); got != exitUsage {
		t.Errorf("exit = %d (%v), want %d (usage)", got, err, exitUsage)
	}
}

func TestUpgradeYankedTargetErrors(t *testing.T) {
	sig, d := newFakeSigstore(t), newFakeDist(t)
	d.publish(t, sig, release{target: "v0.7.0", manifestSemver: "v0.7.0", archive: tarGz(t, []byte("NEW BINARY"))})
	var idx map[string]any
	if err := json.Unmarshal(d.files["index.json"], &idx); err != nil {
		t.Fatal(err)
	}
	idx["semvers"].(map[string]any)["v0.7.0"].(map[string]any)["yanked"] = true
	d.files["index.json"], _ = json.Marshal(idx)
	exe := useDist(t, d, sig.root, "v0.6.0")
	_, err := runUpgrade(t, "-y")
	if got := exitCode(err); got != exitUpstream || !strings.Contains(err.Error(), "yanked") {
		t.Errorf("exit = %d (%v), want %d naming the yank", got, err, exitUpstream)
	}
	if got := readFile(t, exe); got != "OLD BINARY" {
		t.Errorf("binary = %q, want it untouched", got)
	}
}

// Another upgrade may finish between the first version check and the lock.
func TestUpgradeRechecksInstalledVersionUnderLock(t *testing.T) {
	sig, d := newFakeSigstore(t), newFakeDist(t)
	d.publish(t, sig, release{target: "v0.7.0", manifestSemver: "v0.7.0", archive: tarGz(t, []byte("NEW BINARY"))})
	exe := useDist(t, d, sig.root, "v0.6.0")
	installedVersion = func(string) (string, error) { return "v0.7.0", nil }
	out, err := runUpgrade(t, "-y")
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if !strings.Contains(out, "nothing to do") {
		t.Errorf("output = %q, want nothing to do", out)
	}
	if got := readFile(t, exe); got != "OLD BINARY" {
		t.Errorf("binary = %q, want it untouched", got)
	}
	if d.requested("c1i.tar.gz") {
		t.Error("downloaded the release although it was already installed")
	}
}

func TestUpgradeSourceBuildDoesNotReplace(t *testing.T) {
	for _, v := range []string{"dev", "(devel)", "v0.6.1-0.20261005220836-bb1be01ebd69", "v0.0.0-20261005220836-bb1be01ebd69"} {
		t.Run(v, func(t *testing.T) {
			sig, d := newFakeSigstore(t), newFakeDist(t)
			d.publish(t, sig, release{target: "v0.7.0", manifestSemver: "v0.7.0", archive: tarGz(t, []byte("NEW BINARY"))})
			exe := useDist(t, d, sig.root, v)
			out, err := runUpgrade(t, "-y")
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if !strings.Contains(out, "development build") {
				t.Errorf("output = %q", out)
			}
			if got := readFile(t, exe); got != "OLD BINARY" {
				t.Errorf("binary = %q, want it untouched", got)
			}
		})
	}
}
