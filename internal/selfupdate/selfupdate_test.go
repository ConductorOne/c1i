package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
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
	m, err := c.Manifest(context.Background(), idx.Semvers["v0.7.0"].Manifest)
	if err != nil {
		t.Fatalf("Manifest: %v", err)
	}
	if a := m.Assets["linux-amd64"]; a.SHA256 != "abc" || a.Filename == "" {
		t.Errorf("asset = %+v", a)
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
	if _, err := c2.Index(context.Background()); err == nil {
		t.Error("expected an error on a 404 index, got nil")
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

func TestReplaceExecutable(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "c1i")
	if err := os.WriteFile(execPath, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceExecutable(execPath, []byte("NEW")); err != nil {
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

func TestApplyEndToEndAndChecksumGuard(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "c1i")
	if err := os.WriteFile(execPath, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	newBin := []byte("#!c1i-v0.7.0")
	tgz := makeTarGz(t, "c1i", newBin)
	sum := sha256.Sum256(tgz)
	href := "https://dist.example/c1i.tar.gz"

	client := &Client{HTTP: &fakeDoer{resp: map[string]*transport.Response{
		href: {StatusCode: 200, Body: tgz},
	}}}

	// Happy path: replaces the binary.
	asset := Asset{Filename: "c1i-v0.7.0-linux-amd64.tar.gz", SHA256: hex.EncodeToString(sum[:]), Href: href}
	if err := client.Apply(context.Background(), asset, execPath); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got, _ := os.ReadFile(execPath); !bytes.Equal(got, newBin) {
		t.Errorf("binary not replaced: %q", got)
	}

	// Checksum mismatch: aborts, leaves the (now-new) binary untouched.
	_ = os.WriteFile(execPath, []byte("KEEP"), 0o755)
	bad := Asset{Filename: asset.Filename, SHA256: "00", Href: href}
	if err := client.Apply(context.Background(), bad, execPath); err == nil {
		t.Error("Apply with a bad checksum did not error")
	}
	if got, _ := os.ReadFile(execPath); string(got) != "KEEP" {
		t.Errorf("binary changed despite checksum mismatch: %q", got)
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
