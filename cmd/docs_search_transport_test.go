package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// docsOKBody is a minimal 200 body for each docs subcommand.
var docsOKBody = map[string]string{"search": `[]`, "page": `{"content":"ok"}`}

// runDocsCLI runs `c1i docs <args>` against an httptest server standing in for
// the Mintlify API and returns the error plus everything written to stderr.
func runDocsCLI(t *testing.T, h http.HandlerFunc, args ...string) (hits int32, stderr string, err error) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	origSearch, origPage := mintlifySearchURL, mintlifyPageURL
	mintlifySearchURL, mintlifyPageURL = srv.URL+"/search", srv.URL+"/page"
	t.Cleanup(func() { mintlifySearchURL, mintlifyPageURL = origSearch, origPage })

	resetCmds(t, rootCmd, docsSearchCmd, docsPageCmd)
	stderr = captureStderr(t, func() {
		docsSearchCmd.SetContext(t.Context()) // cobra keeps a subcommand's first context
		docsPageCmd.SetContext(t.Context())
		err = runRootWithArgs(t, append([]string{"docs"}, args...))
	})
	return hits, stderr, err
}

func TestDocsDebugTracesRequest(t *testing.T) {
	for _, sub := range []string{"search", "page"} {
		t.Run(sub, func(t *testing.T) {
			_, stderr, err := runDocsCLI(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(docsOKBody[sub]))
			}, sub, "x", "--debug")
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if !strings.Contains(stderr, "> POST http://") {
				t.Errorf("stderr lacks the request line:\n%s", stderr)
			}
		})
	}
}

func TestDocsMaxRetriesRetries503(t *testing.T) {
	for _, sub := range []string{"search", "page"} {
		t.Run(sub, func(t *testing.T) {
			var n int32
			hits, _, err := runDocsCLI(t, func(w http.ResponseWriter, _ *http.Request) {
				if atomic.AddInt32(&n, 1) == 1 {
					w.Header().Set("Retry-After", "0")
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				_, _ = w.Write([]byte(docsOKBody[sub]))
			}, sub, "x", "--max-retries", "1")
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if hits != 2 {
				t.Errorf("hits = %d, want 2 (one retry)", hits)
			}
		})
	}
}

func TestDocsSiteFailureExitsUpstream(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"500":      func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) },
		"404":      func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(404) },
		"non-JSON": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) },
		"redirect": func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/elsewhere", http.StatusFound) },
	}
	for name, h := range cases {
		for _, sub := range []string{"search", "page"} {
			t.Run(name+"/"+sub, func(t *testing.T) {
				_, _, err := runDocsCLI(t, h, sub, "x", "--max-retries", "0")
				if got := exitCode(err); got != exitUpstream {
					t.Errorf("exit = %d (%v), want %d", got, err, exitUpstream)
				}
			})
		}
	}
}
