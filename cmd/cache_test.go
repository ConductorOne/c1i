package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type stubRoundTripper func(*http.Request) (*http.Response, error)

func (f stubRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// runDocsOpenapi runs `docs openapi` with rt serving every request.
func runDocsOpenapi(t *testing.T, rt stubRoundTripper) (string, error) {
	t.Helper()
	orig := http.DefaultClient.Transport
	http.DefaultClient.Transport = rt
	t.Cleanup(func() { http.DefaultClient.Transport = orig })

	out := &bytes.Buffer{}
	prevCtx := docsOpenapiCmd.Context()
	docsOpenapiCmd.SetOut(out)
	docsOpenapiCmd.SetContext(context.Background())
	t.Cleanup(func() {
		docsOpenapiCmd.SetOut(nil)
		docsOpenapiCmd.SetContext(prevCtx)
	})
	err := docsOpenapiCmd.RunE(docsOpenapiCmd, nil)
	return out.String(), err
}

func serveSpec(body string) stubRoundTripper {
	return func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	}
}

// writeExpiredCache writes body as a cache file old enough to force a fetch.
func writeExpiredCache(t *testing.T, cachePath, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	expired := time.Now().Add(-2 * cacheMaxAge)
	if err := os.Chtimes(cachePath, expired, expired); err != nil {
		t.Fatal(err)
	}
}

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
	writeExpiredCache(t, cachePath, stubOpenAPISpec)
	if _, err := runDocsOpenapi(t, serveSpec(stubOpenAPISpec)); err != nil {
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

// With no usable home dir the spec is fetched and used, and the working dir,
// where a relative cache path would land, is neither read, written nor pruned.
func TestOpenAPISpecWithoutHomeDirSkipsCache(t *testing.T) {
	for _, home := range []string{"", "."} {
		t.Run("HOME="+home, func(t *testing.T) {
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			if got, err := os.UserHomeDir(); err == nil && filepath.IsAbs(got) {
				t.Skipf("home dir still resolves to %q", got)
			}
			t.Chdir(t.TempDir())
			relCache := filepath.Join(cacheDirName, "cache", cacheFileName)
			writeExpiredCache(t, relCache, "paths: {}\n")
			stray := filepath.Join(cacheDirName, "cache", "not-ours.txt")
			if err := os.WriteFile(stray, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			before := snapshotTree(t, ".")

			got, err := runDocsOpenapi(t, serveSpec(stubOpenAPISpec))
			if err != nil {
				t.Fatalf("docs openapi: %v", err)
			}
			if got != stubOpenAPISpec {
				t.Errorf("printed %q, want the fetched spec", got)
			}
			if after := snapshotTree(t, "."); after != before {
				t.Errorf("working dir changed:\nbefore: %s\nafter:  %s", before, after)
			}
		})
	}
}

// snapshotTree lists every file under root with its contents.
func snapshotTree(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		b.WriteString(p + "=" + string(data) + ";")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// Bodies a 200 can carry that aren't a spec: a captive portal page, which
// isn't a YAML map, and a JSON object with no paths.
var notASpec = []string{
	"<!DOCTYPE html><html><body>Sign in to Wi-Fi</body></html>",
	`{"message": "sign in required"}`,
}

func TestOpenAPIInvalidBodyKeepsCache(t *testing.T) {
	for _, body := range notASpec {
		cachePath := useTempHome(t)
		writeExpiredCache(t, cachePath, stubOpenAPISpec)

		got, err := runDocsOpenapi(t, serveSpec(body))
		if err != nil {
			t.Fatalf("docs openapi: %v", err)
		}
		if got != stubOpenAPISpec {
			t.Errorf("printed %q, want the cached spec", got)
		}
		if b, err := os.ReadFile(cachePath); err != nil || string(b) != stubOpenAPISpec {
			t.Errorf("cache was overwritten: %q, %v", b, err)
		}
	}
}

func TestOpenAPIInvalidBodyWithoutCacheErrors(t *testing.T) {
	for _, body := range notASpec {
		cachePath := useTempHome(t)

		if _, err := runDocsOpenapi(t, serveSpec(body)); err == nil {
			t.Errorf("expected an error for %q", body)
		}
		if _, err := os.Stat(filepath.Dir(cachePath)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("cache dir was created for %q: %v", body, err)
		}
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

func TestOpenAPIReadErrorFallsBackToCache(t *testing.T) {
	cachePath := useTempHome(t)
	writeExpiredCache(t, cachePath, stubOpenAPISpec)

	got, err := runDocsOpenapi(t, func(r *http.Request) (*http.Response, error) {
		body := io.MultiReader(strings.NewReader("paths:\n"), failingReader{})
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(body), Request: r}, nil
	})
	if err != nil {
		t.Fatalf("docs openapi: %v", err)
	}
	if got != stubOpenAPISpec {
		t.Errorf("printed %q, want the cached spec", got)
	}
}
