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

	"github.com/ConductorOne/c1i/internal/client"
)

type stubRoundTripper func(*http.Request) (*http.Response, error)

func (f stubRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// stubOpenAPIBase makes rt serve every spec fetch.
func stubOpenAPIBase(t *testing.T, rt http.RoundTripper) {
	t.Helper()
	orig := openAPIBase
	openAPIBase = rt
	t.Cleanup(func() { openAPIBase = orig })
}

// setRootFlag sets one of rootCmd's persistent flags for the test.
func setRootFlag(t *testing.T, name, value string) {
	t.Helper()
	f := rootCmd.PersistentFlags().Lookup(name)
	orig, origChanged := f.Value.String(), f.Changed
	if err := f.Value.Set(value); err != nil {
		t.Fatal(err)
	}
	f.Changed = true
	t.Cleanup(func() {
		_ = f.Value.Set(orig)
		f.Changed = origChanged
	})
}

// runDocsOpenapi runs `docs openapi` with rt serving every request and returns
// its stdout and stderr. Retries are off, so one stub response is final.
func runDocsOpenapi(t *testing.T, rt stubRoundTripper) (string, string, error) {
	t.Helper()
	stubOpenAPIBase(t, rt)
	setRootFlag(t, "max-retries", "0")

	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	prevCtx := docsOpenapiCmd.Context()
	docsOpenapiCmd.SetOut(out)
	docsOpenapiCmd.SetErr(errOut)
	docsOpenapiCmd.SetContext(context.Background())
	t.Cleanup(func() {
		docsOpenapiCmd.SetOut(nil)
		docsOpenapiCmd.SetErr(nil)
		docsOpenapiCmd.SetContext(prevCtx)
	})
	err := docsOpenapiCmd.RunE(docsOpenapiCmd, nil)
	return out.String(), errOut.String(), err
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
	if _, _, err := runDocsOpenapi(t, serveSpec(stubOpenAPISpec)); err != nil {
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

			got, _, err := runDocsOpenapi(t, serveSpec(stubOpenAPISpec))
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

		got, _, err := runDocsOpenapi(t, serveSpec(body))
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

		if _, _, err := runDocsOpenapi(t, serveSpec(body)); err == nil {
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

	got, _, err := runDocsOpenapi(t, func(r *http.Request) (*http.Response, error) {
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

// Serving an expired cache because the fetch failed warns on stderr with its
// age and the reason; stdout stays the bare spec.
func TestOpenAPIStaleCacheWarns(t *testing.T) {
	for name, rt := range map[string]stubRoundTripper{
		"network": func(*http.Request) (*http.Response, error) { return nil, errors.New("no route to host") },
		"status": func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: http.NoBody, Request: r}, nil
		},
		"not a spec": serveSpec(notASpec[0]),
	} {
		t.Run(name, func(t *testing.T) {
			writeExpiredCache(t, useTempHome(t), stubOpenAPISpec)

			got, stderr, err := runDocsOpenapi(t, rt)
			if err != nil {
				t.Fatalf("docs openapi: %v", err)
			}
			if got != stubOpenAPISpec {
				t.Errorf("printed %q, want the cached spec", got)
			}
			if want := "Warning: using cached OpenAPI spec from 2d ago ("; !strings.HasPrefix(stderr, want) || strings.Count(stderr, "\n") != 1 {
				t.Errorf("stderr = %q, want one line starting %q", stderr, want)
			}
		})
	}
}

func TestOpenAPIFreshCacheIsSilent(t *testing.T) {
	primeOpenAPICache(t)
	_, stderr, err := runDocsOpenapi(t, func(*http.Request) (*http.Response, error) {
		t.Error("a fresh cache hit sent a request")
		return nil, errors.New("unexpected request")
	})
	if err != nil || stderr != "" {
		t.Errorf("err = %v, stderr = %q; want neither", err, stderr)
	}
}

// The only caller never passes a relative dir today; this pins the guard so a
// future caller can't turn the prune loose on the working directory.
func TestPruneCacheDirSkipsRelativeDir(t *testing.T) {
	t.Chdir(t.TempDir())
	dir := filepath.Join(cacheDirName, "cache")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(dir, "not-ours.txt")
	if err := os.WriteFile(stray, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	pruneCacheDir(dir)
	if _, err := os.Stat(stray); err != nil {
		t.Errorf("prune of a relative dir removed %s: %v", stray, err)
	}
}

func TestFormatAgeClampsFutureMtime(t *testing.T) {
	for d, want := range map[time.Duration]string{
		-5 * time.Minute: "0m",
		90 * time.Second: "1m",
		3 * time.Hour:    "3h",
		50 * time.Hour:   "2d",
	} {
		if got := formatAge(d); got != want {
			t.Errorf("formatAge(%v) = %q, want %q", d, got, want)
		}
	}
}

// runDocsOpenapiRoot runs `c1i docs openapi args...` through rootCmd, so the
// global flags are parsed as a user passes them.
func runDocsOpenapiRoot(t *testing.T, rt http.RoundTripper, args ...string) (string, error) {
	t.Helper()
	stubOpenAPIBase(t, rt)
	for _, name := range []string{"debug", "max-retries"} {
		f := rootCmd.PersistentFlags().Lookup(name)
		t.Cleanup(func() {
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		})
	}
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	prevCtx := docsOpenapiCmd.Context()
	docsOpenapiCmd.SetContext(t.Context()) // cobra keeps a subcommand's first context
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		docsOpenapiCmd.SetContext(prevCtx)
	})
	rootCmd.SetArgs(append([]string{"docs", "openapi"}, args...))
	err := rootCmd.ExecuteContext(t.Context())
	return out.String(), err
}

func TestOpenAPIMaxRetriesFlag(t *testing.T) {
	for flag, want := range map[string]int{"0": 1, "1": 2} {
		t.Run("--max-retries="+flag, func(t *testing.T) {
			useTempHome(t)
			attempts := 0
			_, err := runDocsOpenapiRoot(t, stubRoundTripper(func(r *http.Request) (*http.Response, error) {
				attempts++
				return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: http.NoBody, Request: r}, nil
			}), "--max-retries", flag)
			if err == nil || exitCode(err) != exitServer {
				t.Errorf("err = %v (exit %d), want exit %d", err, exitCode(err), exitServer)
			}
			if attempts != want {
				t.Errorf("sent %d requests, want %d", attempts, want)
			}
		})
	}
}

func TestOpenAPIDebugTracesFetch(t *testing.T) {
	useTempHome(t)
	var err error
	trace := captureStderr(t, func() {
		_, err = runDocsOpenapiRoot(t, serveSpec(stubOpenAPISpec), "--debug")
	})
	if err != nil {
		t.Fatalf("docs openapi --debug: %v", err)
	}
	if want := "> GET " + openapiURL; !strings.Contains(trace, want) {
		t.Errorf("stderr = %q, want a trace line %q", trace, want)
	}
}

func TestOpenAPIRefusesCrossHostRedirect(t *testing.T) {
	useTempHome(t)
	_, err := runDocsOpenapiRoot(t, stubRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != openapiURL {
			t.Errorf("followed a redirect to %s", r.URL)
			return serveSpec(stubOpenAPISpec)(r)
		}
		h := http.Header{"Location": {"https://elsewhere.example/api/openapi.yaml"}}
		return &http.Response{StatusCode: http.StatusFound, Header: h, Body: http.NoBody, Request: r}, nil
	}))
	if err == nil || !strings.Contains(err.Error(), "refusing to follow redirect") || exitCode(err) != exitUpstream {
		t.Errorf("err = %v (exit %d), want a refused redirect, exit %d", err, exitCode(err), exitUpstream)
	}
}

// A body over the cap is a failed fetch, even one that would parse as a spec.
func TestOpenAPIOverCapBody(t *testing.T) {
	filler := "# filler\n"
	body := "paths: {}\n" + strings.Repeat(filler, maxOpenAPISpecBytes/len(filler)+1)
	t.Run("no cache", func(t *testing.T) {
		useTempHome(t)
		_, _, err := runDocsOpenapi(t, serveSpec(body))
		if err == nil || exitCode(err) != exitUpstream {
			t.Errorf("err = %v (exit %d), want exit %d", err, exitCode(err), exitUpstream)
		}
	})
	t.Run("expired cache", func(t *testing.T) {
		writeExpiredCache(t, useTempHome(t), stubOpenAPISpec)
		got, stderr, err := runDocsOpenapi(t, serveSpec(body))
		if err != nil {
			t.Fatalf("docs openapi: %v", err)
		}
		if got != stubOpenAPISpec || !strings.Contains(stderr, "Warning: using cached OpenAPI spec") {
			t.Errorf("stdout = %.40q, stderr = %q; want the cached spec and a warning", got, stderr)
		}
	})
}

// A fresh cache that can't be read is skipped, not returned as an error.
func TestOpenAPIUnreadableFreshCacheFetches(t *testing.T) {
	if err := os.MkdirAll(useTempHome(t), 0o700); err != nil {
		t.Fatal(err)
	}
	got, _, err := runDocsOpenapi(t, serveSpec(stubOpenAPISpec))
	if err != nil {
		t.Fatalf("docs openapi: %v", err)
	}
	if got != stubOpenAPISpec {
		t.Errorf("printed %q, want the fetched spec", got)
	}
}

// unsetRetriesEnv clears C1I_MAX_RETRIES for the test.
func unsetRetriesEnv(t *testing.T) {
	t.Helper()
	t.Setenv("C1I_MAX_RETRIES", "")
	_ = os.Unsetenv("C1I_MAX_RETRIES")
}

func serve503(attempts *int) stubRoundTripper {
	return func(r *http.Request) (*http.Response, error) {
		*attempts++
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: http.NoBody, Request: r}, nil
	}
}

// With a cache to fall back on, retries only delay the answer.
func TestOpenAPIStaleCacheMakesOneAttempt(t *testing.T) {
	unsetRetriesEnv(t)
	writeExpiredCache(t, useTempHome(t), stubOpenAPISpec)
	attempts := 0
	out, err := runDocsOpenapiRoot(t, serve503(&attempts))
	if err != nil {
		t.Fatalf("docs openapi: %v", err)
	}
	if attempts != 1 || !strings.Contains(out, "Warning: using cached OpenAPI spec") {
		t.Errorf("sent %d requests, output %.200q; want 1 and the stale-cache warning", attempts, out)
	}
}

func TestOpenAPIExplicitRetriesApplyWithCache(t *testing.T) {
	unsetRetriesEnv(t)
	writeExpiredCache(t, useTempHome(t), stubOpenAPISpec)
	attempts := 0
	if _, err := runDocsOpenapiRoot(t, serve503(&attempts), "--max-retries", "2"); err != nil {
		t.Fatalf("docs openapi: %v", err)
	}
	if attempts != 3 {
		t.Errorf("sent %d requests, want 3", attempts)
	}
}

func TestOpenAPIRetries(t *testing.T) {
	unsetRetriesEnv(t)
	cachePath := useTempHome(t)
	if got := openAPIRetries(cachePath); got != client.DefaultMaxRetries {
		t.Errorf("no cache: %d retries, want the default %d", got, client.DefaultMaxRetries)
	}
	writeExpiredCache(t, cachePath, stubOpenAPISpec)
	if got := openAPIRetries(cachePath); got != 0 {
		t.Errorf("cache: %d retries, want 0", got)
	}
	t.Setenv("C1I_MAX_RETRIES", "2")
	if got := openAPIRetries(cachePath); got != 2 {
		t.Errorf("cache and C1I_MAX_RETRIES=2: %d retries, want 2", got)
	}
}
