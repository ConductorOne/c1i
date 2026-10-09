package cmd

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type stubRoundTripper func(*http.Request) (*http.Response, error)

func (f stubRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// useTempHome points the home dir at a temp dir and returns the cache path.
// USERPROFILE is what os.UserHomeDir reads on Windows.
func useTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return filepath.Join(home, cacheDirName, "cache", cacheFileName)
}

// refreshOpenAPICache runs `docs openapi` against an expired cache so it
// fetches a stub spec and rewrites the cache.
func refreshOpenAPICache(t *testing.T, cacheDir string) {
	t.Helper()
	cachePath := filepath.Join(cacheDir, cacheFileName)
	if err := os.WriteFile(cachePath, []byte(stubOpenAPISpec), 0o600); err != nil {
		t.Fatal(err)
	}
	expired := time.Now().Add(-2 * cacheMaxAge)
	if err := os.Chtimes(cachePath, expired, expired); err != nil {
		t.Fatal(err)
	}

	orig := http.DefaultClient.Transport
	http.DefaultClient.Transport = stubRoundTripper(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(stubOpenAPISpec)),
			Request:    r,
		}, nil
	})
	t.Cleanup(func() { http.DefaultClient.Transport = orig })

	prevCtx := docsOpenapiCmd.Context()
	docsOpenapiCmd.SetOut(&bytes.Buffer{})
	docsOpenapiCmd.SetContext(context.Background())
	t.Cleanup(func() {
		docsOpenapiCmd.SetOut(nil)
		docsOpenapiCmd.SetContext(prevCtx)
	})
	if err := docsOpenapiCmd.RunE(docsOpenapiCmd, nil); err != nil {
		t.Fatalf("docs openapi: %v", err)
	}
	info, err := os.Stat(cachePath)
	if err != nil || time.Since(info.ModTime()) > time.Minute {
		t.Fatalf("cache was not rewritten: %v", err)
	}
}

func TestCacheWritePrunesLeftoverFiles(t *testing.T) {
	cacheDir := filepath.Dir(useTempHome(t))
	home := filepath.Dir(filepath.Dir(cacheDir))
	for _, d := range []string{"subdir", "empty-subdir"} {
		if err := os.MkdirAll(filepath.Join(cacheDir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"openapi.yaml", ".tmp-leftover"} {
		if err := os.WriteFile(filepath.Join(cacheDir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// None of these is a cache file, so all must survive: a link's target, a
	// file inside a subdirectory, and state beside the cache dir.
	outside := filepath.Join(t.TempDir(), "outside.txt")
	inner := filepath.Join(cacheDir, "subdir", "inner.txt")
	sibling := filepath.Join(home, cacheDirName, "other-state.json")
	for _, p := range []string{outside, inner, sibling} {
		if err := os.WriteFile(p, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(cacheDir, "link")); err != nil {
		t.Logf("symlinks unavailable, link entry not covered: %v", err)
	}

	refreshOpenAPICache(t, cacheDir)

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	want := []string{cacheFileName, "empty-subdir", "subdir"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("cache dir holds %v, want %v", got, want)
	}
	for _, p := range []string{outside, inner, sibling} {
		if b, err := os.ReadFile(p); err != nil || string(b) != "keep" {
			t.Errorf("%s was removed or changed: %q, %v", p, b, err)
		}
	}
}

// A cache dir the user redirected elsewhere may hold files c1i doesn't own.
func TestCachePruneSkipsSymlinkedCacheDir(t *testing.T) {
	home := filepath.Dir(filepath.Dir(filepath.Dir(useTempHome(t))))
	target := t.TempDir()
	stray := filepath.Join(target, "not-ours.txt")
	if err := os.WriteFile(stray, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, cacheDirName), 0o700); err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(home, cacheDirName, "cache")
	if err := os.Symlink(target, cacheDir); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	refreshOpenAPICache(t, cacheDir)

	if _, err := os.Stat(stray); err != nil {
		t.Errorf("file behind a symlinked cache dir was removed: %v", err)
	}
}

// With no home dir the cache path is relative, so it lands in the working dir.
func TestCachePruneSkipsRelativeCacheDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("an empty HOME doesn't unset the home dir on Windows")
	}
	t.Setenv("HOME", "")
	t.Chdir(t.TempDir())
	cacheDir := filepath.Join(cacheDirName, "cache")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(cacheDir, "not-ours.txt")
	if err := os.WriteFile(stray, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	refreshOpenAPICache(t, cacheDir)

	if _, err := os.Stat(stray); err != nil {
		t.Errorf("file in a working-dir-relative cache dir was removed: %v", err)
	}
}
