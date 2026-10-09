package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ConductorOne/c1i/internal/transport"
)

// fakeDoer serves canned transport.Responses keyed by URL, standing in for the
// distribution center so no test touches the network.
type fakeDoer struct {
	resp map[string]*transport.Response
	err  error
}

func (f *fakeDoer) Do(req *http.Request) (*transport.Response, error) {
	if f.err != nil {
		return nil, f.err
	}
	if r, ok := f.resp[req.URL.String()]; ok {
		return r, nil
	}
	return &transport.Response{StatusCode: http.StatusNotFound}, nil
}

func jsonResp(body string) *transport.Response {
	return &transport.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       []byte(body),
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"v0.6.0", "v0.7.0", -1, true},
		{"v0.7.0", "v0.6.0", 1, true},
		{"v0.7.0", "v0.7.0", 0, true},
		{"v0.7.0", "0.7.0", 0, true}, // v-prefix optional
		{"v0.10.0", "v0.9.0", 1, true},
		{"v1.0.0", "v1.0.0-rc.1", 1, true}, // release outranks its prerelease
		{"v1.0.0-rc.1", "v1.0.0-rc.2", -1, true},
		{"v1.0.0-rc.10", "v1.0.0-rc.2", 1, true},  // numeric identifiers compare numerically, not lexically
		{"v1.0.0-rc.2", "v1.0.0-rc.10", -1, true}, // symmetric
		{"v1.0.0-rc.10", "v1.0.0-rc.10", 0, true},
		{"v1.0.0-alpha", "v1.0.0-alpha.1", -1, true}, // shorter prefix outranked by longer
		{"v1.0.0-alpha.1", "v1.0.0-beta", -1, true},  // non-numeric lexical
		{"v1.0.0-rc.1", "v1.0.0-rc.alpha", -1, true}, // numeric ranks below non-numeric
		{"dev", "v0.7.0", 0, false},
		{"v0.7", "v0.7.0", 0, false}, // not three components
		{"", "v0.7.0", 0, false},
	}
	for _, c := range cases {
		got, ok := CompareVersions(c.a, c.b)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("CompareVersions(%q,%q) = (%d,%v), want (%d,%v)", c.a, c.b, got, ok, c.want, c.ok)
		}
	}
}

func TestDetect(t *testing.T) {
	if m, _ := Detect(`C:\Users\x\c1i.exe`, "windows"); m != Windows {
		t.Errorf("windows -> %v, want Windows", m)
	}
	if m, _ := Detect("/opt/homebrew/Cellar/c1i/0.7.0/bin/c1i", "darwin"); m != Homebrew {
		t.Errorf("Cellar path -> %v, want Homebrew", m)
	}
	if m, _ := Detect("/usr/local/bin/c1i", "linux"); m != Standalone {
		t.Errorf("/usr/local/bin -> %v, want Standalone", m)
	}
	// go install: binary under GOBIN.
	gobin := t.TempDir()
	t.Setenv("GOBIN", gobin)
	if m, hint := Detect(filepath.Join(gobin, "c1i"), "linux"); m != GoInstall {
		t.Errorf("GOBIN path -> %v, want GoInstall", m)
	} else if hint == "" {
		t.Error("GoInstall returned no remediation hint")
	}
}

func TestMethodNamesAndCommands(t *testing.T) {
	cases := []struct {
		m             Method
		name, command string
	}{
		{Standalone, "standalone", ""},
		{Homebrew, "homebrew", "brew upgrade c1i"},
		{GoInstall, "go-install", "go install github.com/ConductorOne/c1i@latest"},
		{Docker, "container", "docker pull public.ecr.aws/conductorone/c1i"},
		{SystemInstall, "system", ""},
		{Windows, "windows", ""},
	}
	for _, c := range cases {
		if c.m.String() != c.name || c.m.Command() != c.command {
			t.Errorf("%d: (%q, %q), want (%q, %q)", c.m, c.m.String(), c.m.Command(), c.name, c.command)
		}
	}
}

// A go.mod in the caller's cwd that needs a newer toolchain must not make
// readGoEnv fail or download one.
func TestReadGoEnvIgnoresCallerModule(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain on PATH")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n\ngo 1.999\n\ntoolchain go1.999.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("GOTOOLCHAIN", "auto")
	t.Setenv("GOPROXY", "off")
	if env := readGoEnv(); env.GOPATH == "" {
		t.Error("readGoEnv returned an empty GOPATH")
	}
}

// isolateGoEnv points everything readGoEnv consults at an empty temp home and
// returns that home and the config dir it implies.
func isolateGoEnv(t *testing.T) (home, configDir string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("AppData", filepath.Join(home, "config"))
	for _, k := range []string{"GOBIN", "GOPATH", "GOENV"} {
		t.Setenv(k, "")
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	return home, configDir
}

func TestDetectGoInstallSources(t *testing.T) {
	writeEnvFile := func(t *testing.T, path, gobin string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		// The format `go env -w GOBIN=...` writes.
		if err := os.WriteFile(path, []byte("GOFLAGS=-mod=mod\nGOBIN="+gobin+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, c := range map[string]struct {
		setup func(t *testing.T, home, configDir string) string // returns the binary's path
		want  Method
	}{
		"GOBIN env var": {func(t *testing.T, home, _ string) string {
			t.Setenv("GOBIN", filepath.Join(home, "gobin"))
			return filepath.Join(home, "gobin", "c1i")
		}, GoInstall},
		"GOPATH list": {func(t *testing.T, home, _ string) string {
			t.Setenv("GOPATH", filepath.Join(home, "a")+string(filepath.ListSeparator)+filepath.Join(home, "b"))
			return filepath.Join(home, "b", "bin", "c1i")
		}, GoInstall},
		"go env file": {func(t *testing.T, home, configDir string) string {
			writeEnvFile(t, filepath.Join(configDir, "go", "env"), filepath.Join(home, "persisted"))
			return filepath.Join(home, "persisted", "c1i")
		}, GoInstall},
		"GOENV file": {func(t *testing.T, home, _ string) string {
			writeEnvFile(t, filepath.Join(home, "custom-env"), filepath.Join(home, "persisted"))
			t.Setenv("GOENV", filepath.Join(home, "custom-env"))
			return filepath.Join(home, "persisted", "c1i")
		}, GoInstall},
		"GOENV=off": {func(t *testing.T, home, configDir string) string {
			writeEnvFile(t, filepath.Join(configDir, "go", "env"), filepath.Join(home, "persisted"))
			t.Setenv("GOENV", "off")
			return filepath.Join(home, "persisted", "c1i")
		}, Standalone},
		"default GOPATH": {func(t *testing.T, home, _ string) string {
			return filepath.Join(home, "go", "bin", "c1i")
		}, GoInstall},
		"elsewhere": {func(t *testing.T, home, _ string) string {
			return filepath.Join(home, "downloads", "c1i")
		}, Standalone},
	} {
		t.Run(name, func(t *testing.T) {
			home, configDir := isolateGoEnv(t)
			path := c.setup(t, home, configDir)
			if m, _ := Detect(path, "darwin"); m != c.want {
				t.Errorf("%s -> %v, want %v", path, m, c.want)
			}
		})
	}
}

// Running the go command writes telemetry counters under the config dir.
func TestReadGoEnvWritesNoTelemetry(t *testing.T) {
	_, configDir := isolateGoEnv(t)
	readGoEnv()
	if _, err := os.Stat(filepath.Join(configDir, "go", "telemetry")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("readGoEnv left go/telemetry under the config dir: %v", err)
	}
}

func TestDetectSystemAndPersistedGoInstall(t *testing.T) {
	if m, hint := Detect("/usr/bin/c1i", "darwin"); m != SystemInstall || hint == "" {
		t.Errorf("/usr/bin -> (%v, %q), want SystemInstall with a remediation", m, hint)
	}

	original := readGoEnv
	readGoEnv = func() goEnv {
		return goEnv{GOBIN: "/opt/custom-go/bin", GOPATH: "/work/a:/work/b"}
	}
	t.Cleanup(func() { readGoEnv = original })
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")

	for _, path := range []string{"/opt/custom-go/bin/c1i", "/work/b/bin/c1i"} {
		if m, hint := Detect(path, "darwin"); m != GoInstall || hint == "" {
			t.Errorf("%s -> (%v, %q), want GoInstall with a remediation", path, m, hint)
		}
	}
}

func TestClientIndexAndManifest(t *testing.T) {
	base := "https://dist.example/releases/ConductorOne/c1i"
	index := `{"channels":{"stable":"v0.6.0","latest":"v0.7.0"},"semvers":{"v0.7.0":{"yanked":false,"manifest":"` + base + `/v0.7.0/manifest.json"}}}`
	manifest := `{"semver":"v0.7.0","assets":{"linux-amd64":{"filename":"c1i-v0.7.0-linux-amd64.tar.gz","sha256":"abc","href":"` + base + `/v0.7.0/c1i-v0.7.0-linux-amd64.tar.gz"}}}`
	c := &Client{BaseURL: base, HTTP: &fakeDoer{resp: map[string]*transport.Response{
		base + "/index.json":           jsonResp(index),
		base + "/v0.7.0/manifest.json": jsonResp(manifest),
	}}}

	idx, err := c.Index(context.Background())
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if idx.Channels["stable"] != "v0.6.0" || idx.Channels["latest"] != "v0.7.0" {
		t.Errorf("channels = %v", idx.Channels)
	}
	m, raw, err := c.ManifestRaw(context.Background(), idx.Semvers["v0.7.0"].Manifest)
	if err != nil {
		t.Fatalf("ManifestRaw: %v", err)
	}
	if a := m.Assets["linux-amd64"]; a.SHA256 != "abc" || a.Filename == "" {
		t.Errorf("asset = %+v", a)
	}
	if string(raw) != manifest {
		t.Errorf("raw = %q, want the exact bytes served", raw)
	}
}

func TestClientRejectsHTMLShellAndNon200(t *testing.T) {
	base := "https://dist.example/releases/ConductorOne/c1i"
	shell := &transport.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: []byte("<html></html>")}
	c := &Client{BaseURL: base, HTTP: &fakeDoer{resp: map[string]*transport.Response{base + "/index.json": shell}}}
	if _, err := c.Index(context.Background()); err == nil {
		t.Error("expected an error decoding the SPA HTML shell, got nil")
	}

	c2 := &Client{BaseURL: base, HTTP: &fakeDoer{}} // everything 404s
	_, err := c2.Index(context.Background())
	var apiErr *transport.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound || apiErr.Path != "/releases/ConductorOne/c1i/index.json" {
		t.Errorf("404 index error = %#v, want a *transport.APIError for the index path", err)
	}
}

func TestVerifySHA256(t *testing.T) {
	data := []byte("hello")
	sum := sha256.Sum256(data)
	good := hex.EncodeToString(sum[:])
	if err := verifySHA256(data, good); err != nil {
		t.Errorf("matching sha256 errored: %v", err)
	}
	if err := verifySHA256(data, "deadbeef"); err == nil {
		t.Error("mismatched sha256 did not error")
	}
	if err := verifySHA256(data, ""); err == nil {
		t.Error("empty sha256 did not error")
	}
}

func TestExtractBinary(t *testing.T) {
	want := []byte("#!c1i-binary")
	if got, err := extractBinary(makeTarGz(t, "c1i", want), "c1i-v0.7.0-linux-amd64.tar.gz"); err != nil || !bytes.Equal(got, want) {
		t.Errorf("tar.gz extract = (%q,%v)", got, err)
	}
	if got, err := extractBinary(makeZip(t, "c1i", want), "c1i-v0.7.0-darwin-arm64.zip"); err != nil || !bytes.Equal(got, want) {
		t.Errorf("zip extract = (%q,%v)", got, err)
	}
	// binary under a top-level dir is still found.
	if got, err := extractBinary(makeTarGz(t, "c1i-v0.7.0/c1i", want), "x.tar.gz"); err != nil || !bytes.Equal(got, want) {
		t.Errorf("nested tar.gz extract = (%q,%v)", got, err)
	}
	if _, err := extractBinary(makeTarGz(t, "README.md", want), "x.tar.gz"); err == nil {
		t.Error("archive without a c1i entry did not error")
	}
	if _, err := extractBinary([]byte("x"), "x.rar"); err == nil {
		t.Error("unsupported archive extension did not error")
	}
}

func TestReadBoundedRejectsOverflow(t *testing.T) {
	if _, err := readBounded(bytes.NewReader([]byte("1234")), 3); err == nil {
		t.Fatal("expected oversized binary to be rejected")
	}
	got, err := readBounded(bytes.NewReader([]byte("123")), 3)
	if err != nil || string(got) != "123" {
		t.Fatalf("readBounded exact limit = (%q, %v), want (123, nil)", got, err)
	}
}

func TestReplaceExecutable(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "c1i")
	if err := os.WriteFile(execPath, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceExecutable(execPath, []byte("NEW")); err != nil {
		t.Fatalf("replace: %v", err)
	}
	got, _ := os.ReadFile(execPath)
	if string(got) != "NEW" {
		t.Errorf("content = %q, want NEW", got)
	}
	if fi, _ := os.Stat(execPath); fi.Mode().Perm()&0o100 == 0 {
		t.Error("replaced binary is not executable")
	}
	// No leftover temp files in the dir.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("dir has %d entries, want 1 (temp file leaked)", len(entries))
	}
}

func TestLockExecutableRejectsConcurrentUpgrade(t *testing.T) {
	execPath := filepath.Join(t.TempDir(), "c1i")
	unlock, err := LockExecutable(execPath)
	if err != nil {
		t.Fatalf("first LockExecutable: %v", err)
	}
	t.Cleanup(unlock)
	if _, err := LockExecutable(execPath); err == nil {
		t.Fatal("second LockExecutable succeeded while the first lock was held")
	}
}

func TestLockExecutableLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "c1i")
	if err := os.WriteFile(execPath, []byte("BIN"), 0o755); err != nil {
		t.Fatal(err)
	}
	unlock, err := LockExecutable(execPath)
	if err != nil {
		t.Fatalf("LockExecutable: %v", err)
	}
	unlock()
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("install dir holds %v after unlock, want only c1i", names)
	}
	// Released: a later upgrade can take the lock.
	unlock, err = LockExecutable(execPath)
	if err != nil {
		t.Fatalf("LockExecutable after unlock: %v", err)
	}
	unlock()
}

func TestFetchBinaryAndChecksumGuard(t *testing.T) {
	newBin := []byte("#!c1i-v0.7.0")
	tgz := makeTarGz(t, "c1i", newBin)
	sum := sha256.Sum256(tgz)
	// Href must share the client's base host (URL pinning), so set BaseURL to
	// match rather than relying on the production default.
	href := "https://dist.example/c1i.tar.gz"

	client := &Client{BaseURL: "https://dist.example", HTTP: &fakeDoer{resp: map[string]*transport.Response{
		href: {StatusCode: 200, Body: tgz},
	}}}

	asset := Asset{Filename: "c1i-v0.7.0-linux-amd64.tar.gz", SHA256: hex.EncodeToString(sum[:]), Href: href}
	if got, err := client.FetchBinary(context.Background(), asset); err != nil || !bytes.Equal(got, newBin) {
		t.Fatalf("FetchBinary = (%q, %v), want the extracted binary", got, err)
	}

	bad := Asset{Filename: asset.Filename, SHA256: "00", Href: href}
	if got, err := client.FetchBinary(context.Background(), bad); err == nil {
		t.Errorf("FetchBinary with a bad checksum returned %q, want an error", got)
	}
}

func TestValidateURLRejectsOffHostAndNonHTTPS(t *testing.T) {
	base := "https://dist.example/releases/ConductorOne/c1i"
	c := &Client{BaseURL: base}
	cases := []struct {
		url     string
		wantErr bool
	}{
		{"https://dist.example/releases/ConductorOne/c1i/v1/manifest.json", false},
		{"https://evil.example/manifest.json", true},          // off-host
		{"http://dist.example/manifest.json", true},           // non-https
		{"https://dist.example.evil.com/manifest.json", true}, // suffix trick, different host
		{"ftp://dist.example/manifest.json", true},            // wrong scheme
	}
	for _, tc := range cases {
		err := c.validateURL(tc.url)
		if tc.wantErr && err == nil {
			t.Errorf("validateURL(%q) = nil, want error", tc.url)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("validateURL(%q) = %v, want nil", tc.url, err)
		}
	}
}

func TestFetchBinaryRejectsOffHostHref(t *testing.T) {
	d := &recordingDoer{resp: &transport.Response{StatusCode: 200}}
	c := &Client{BaseURL: "https://dist.example", HTTP: d}
	asset := Asset{Filename: "c1i.tar.gz", SHA256: "abc", Href: "https://evil.example/c1i.tar.gz"}
	if _, err := c.FetchBinary(context.Background(), asset); err == nil {
		t.Fatal("FetchBinary followed an off-host href; expected a refusal")
	}
	if len(d.urls) != 0 {
		t.Errorf("requests = %v, want none", d.urls)
	}
}

func TestFromZipSkipsSymlinkEntry(t *testing.T) {
	// A zip whose "c1i" entry is a symlink must not be treated as the binary;
	// only a regular-file c1i counts (mirrors fromTarGz's tar.TypeReg check).
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	// Symlink entry named c1i.
	symHdr := &zip.FileHeader{Name: "c1i"}
	symHdr.SetMode(os.ModeSymlink | 0o777)
	w, err := zw.CreateHeader(symHdr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("/etc/passwd")); err != nil {
		t.Fatal(err)
	}
	_ = zw.Close()

	if _, err := fromZip(buf.Bytes()); err == nil {
		t.Fatal("fromZip returned a binary for a symlink-only c1i entry; expected 'no c1i binary found'")
	}

	// Now add a real regular-file c1i alongside the symlink: it must be found.
	buf.Reset()
	zw = zip.NewWriter(&buf)
	w, _ = zw.CreateHeader(symHdr)
	_, _ = w.Write([]byte("/etc/passwd"))
	want := []byte("#!real-c1i")
	rw, err := zw.Create("dir/c1i")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rw.Write(want); err != nil {
		t.Fatal(err)
	}
	_ = zw.Close()
	got, err := fromZip(buf.Bytes())
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("fromZip with a real c1i = (%q, %v), want (%q, nil)", got, err, want)
	}
}

func TestInContainerDetectsPodmanMarker(t *testing.T) {
	// Point the marker list at a temp file (no /run/.containerenv on the host)
	// to prove the Podman marker path is honored.
	dir := t.TempDir()
	marker := filepath.Join(dir, ".containerenv")
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	origMarkers, origCgroup := containerMarkerFiles, containerCgroupFile
	t.Cleanup(func() { containerMarkerFiles, containerCgroupFile = origMarkers, origCgroup })

	// No markers present, cgroup file absent: not a container.
	containerMarkerFiles = []string{filepath.Join(dir, "absent")}
	containerCgroupFile = filepath.Join(dir, "absent-cgroup")
	if inContainer() {
		t.Error("inContainer() = true with no markers present")
	}

	// Podman marker present: container.
	containerMarkerFiles = []string{filepath.Join(dir, "absent"), marker}
	if !inContainer() {
		t.Error("inContainer() = false despite the Podman marker file")
	}
}

func makeTarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func makeZip(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
	_ = zw.Close()
	return buf.Bytes()
}
