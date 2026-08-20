package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestClient_RedirectPathChanged_Refused drives every 3xx status the guard
// covers through an httptest.Server whose redirect changes the resource
// path (a collection, not the requested id), and asserts every one is
// refused with a *RedirectError and the target is never reached.
func TestClient_RedirectPathChanged_Refused(t *testing.T) {
	for _, status := range []int{
		http.StatusMovedPermanently,  // 301
		http.StatusFound,             // 302
		http.StatusSeeOther,          // 303
		http.StatusTemporaryRedirect, // 307
		http.StatusPermanentRedirect, // 308
	} {
		t.Run(fmt.Sprintf("%d", status), func(t *testing.T) {
			var targetCalled bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.EscapedPath() {
				case "/x/%2F":
					w.Header().Set("Location", "/x/")
					w.WriteHeader(status)
				case "/x/":
					targetCalled = true
					_, _ = w.Write([]byte(`{"list":["should","never","be","seen"]}`))
				default:
					t.Fatalf("unexpected request to %s", r.URL.EscapedPath())
				}
			}))
			defer srv.Close()

			c := newTestClient(srv, 0)
			body, err := c.Get(context.Background(), Path("/x/%s", "/"), nil)
			var redirErr *RedirectError
			if !errors.As(err, &redirErr) {
				t.Fatalf("status %d: error = %T (%v), want *RedirectError", status, err, err)
			}
			if body != nil {
				t.Errorf("status %d: body = %q, want nil (no body on refusal)", status, body)
			}
			if targetCalled {
				t.Errorf("status %d: the path-changed target was reached; it must be refused", status)
			}
		})
	}
}

// TestClient_RedirectDifferentHost_SamePath_Followed proves a redirect to a
// different host that preserves the request path — pure host
// canonicalization — is followed rather than refused: it drives two real
// httptest servers so the "follow" is a genuine second HTTP round trip, not
// a helper called in isolation.
func TestClient_RedirectDifferentHost_SamePath_Followed(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/x/abc" {
			t.Fatalf("target got unexpected path %s", r.URL.EscapedPath())
		}
		_, _ = w.Write([]byte(`{"id":"abc"}`))
	}))
	defer target.Close()

	initial := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/x/abc":
			w.Header().Set("Location", target.URL+"/x/abc")
			w.WriteHeader(http.StatusMovedPermanently)
		default:
			t.Fatalf("initial got unexpected path %s", r.URL.EscapedPath())
		}
	}))
	defer initial.Close()

	c := newTestClient(initial, 0)
	body, err := c.Get(context.Background(), "/x/abc", nil)
	if err != nil {
		t.Fatalf("unexpected error following a same-path, different-host redirect: %v", err)
	}
	if string(body) != `{"id":"abc"}` {
		t.Errorf("body = %s, want the target server's body", body)
	}
}

// schemeSwappingTransport wraps a real http.Transport and rewrites the
// request's scheme back to "http" immediately before dialing. It exists only
// so this test can prove redirectTripper *attempts* to follow a scheme-only
// change end-to-end without standing up a mutually-trusted TLS listener on
// the exact host:port httptest.NewServer already bound (the scheme is
// otherwise untestable in isolation from a real network hop, since a real
// "https" dial to a plain-HTTP httptest.Server would fail on the TLS
// handshake for reasons unrelated to redirectTripper's own logic).
type schemeSwappingTransport struct{ next *http.Transport }

func (s *schemeSwappingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	return s.next.RoundTrip(req)
}

// TestClient_RedirectSchemeOnly_SamePath_Followed proves a redirect that
// changes only the scheme (host and path identical) is followed, not
// refused, by driving a real httptest.Server redirect whose Location is an
// absolute "https" URL at the same host:port.
func TestClient_RedirectSchemeOnly_SamePath_Followed(t *testing.T) {
	// The server can't tell the two hops apart by scheme (schemeSwappingTransport
	// rewrites every dial back to plain HTTP, so it never sees a real TLS
	// handshake) or by path (scheme-only means the path is identical on both
	// hops), so it tracks "have I already redirected" explicitly instead.
	var redirected bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/x/abc" {
			t.Fatalf("unexpected request to %s", r.URL.EscapedPath())
		}
		if !redirected {
			redirected = true
			u := *r.URL
			u.Scheme = "https"
			u.Host = r.Host
			w.Header().Set("Location", u.String())
			w.WriteHeader(http.StatusMovedPermanently)
			return
		}
		_, _ = w.Write([]byte(`{"id":"abc"}`))
	}))
	defer srv.Close()

	hc := &http.Client{Transport: &redirectTripper{next: &schemeSwappingTransport{next: http.DefaultTransport.(*http.Transport)}}}
	c := &Client{httpClient: hc, baseURL: srv.URL, maxRetries: 0}
	body, err := c.Get(context.Background(), "/x/abc", nil)
	if err != nil {
		t.Fatalf("unexpected error following a scheme-only redirect: %v", err)
	}
	if string(body) != `{"id":"abc"}` {
		t.Errorf("body = %s, want the server's body", body)
	}
}

// TestClient_RedirectRelativeLocation covers a relative Location header:
// resolving to the same path (here, a query-only reference) must be
// followed; resolving to a different path must be refused. Both are driven
// through a real httptest.Server.
func TestClient_RedirectRelativeLocation(t *testing.T) {
	t.Run("resolves to same path: followed", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.EscapedPath() == "/x/abc" && r.URL.RawQuery == "":
				w.Header().Set("Location", "?trace=1") // relative, empty path -> same path, new query
				w.WriteHeader(http.StatusFound)
			case r.URL.EscapedPath() == "/x/abc" && r.URL.RawQuery == "trace=1":
				_, _ = w.Write([]byte(`{"id":"abc"}`))
			default:
				t.Fatalf("unexpected request to %s?%s", r.URL.EscapedPath(), r.URL.RawQuery)
			}
		}))
		defer srv.Close()

		c := newTestClient(srv, 0)
		body, err := c.Get(context.Background(), "/x/abc", nil)
		if err != nil {
			t.Fatalf("unexpected error following a same-path relative redirect: %v", err)
		}
		if string(body) != `{"id":"abc"}` {
			t.Errorf("body = %s, want the server's body", body)
		}
	})

	t.Run("resolves to different path: refused", func(t *testing.T) {
		var otherCalled bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.EscapedPath() {
			case "/x/abc":
				w.Header().Set("Location", "abc2") // relative, resolves to /x/abc2
				w.WriteHeader(http.StatusFound)
			case "/x/abc2":
				otherCalled = true
				_, _ = w.Write([]byte(`{"id":"abc2"}`))
			default:
				t.Fatalf("unexpected request to %s", r.URL.EscapedPath())
			}
		}))
		defer srv.Close()

		c := newTestClient(srv, 0)
		_, err := c.Get(context.Background(), "/x/abc", nil)
		var redirErr *RedirectError
		if !errors.As(err, &redirErr) {
			t.Fatalf("error = %T (%v), want *RedirectError", err, err)
		}
		if otherCalled {
			t.Error("the different-path target was reached; a relative Location resolving elsewhere must be refused")
		}
	})
}

// TestClient_RedirectLoop_SamePath_Bounded proves a chain of same-path
// redirects (each individually allowed) terminates with a clear
// *RedirectLoopError instead of hanging, and that the server is called
// exactly maxRedirectHops times — never more.
func TestClient_RedirectLoop_SamePath_Bounded(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Location", "/loop") // same path, forever
		w.WriteHeader(http.StatusMovedPermanently)
	}))
	defer srv.Close()

	c := newTestClient(srv, 0)
	_, err := c.Get(context.Background(), "/loop", nil)
	var loopErr *RedirectLoopError
	if !errors.As(err, &loopErr) {
		t.Fatalf("error = %T (%v), want *RedirectLoopError", err, err)
	}
	if calls != maxRedirectHops {
		t.Errorf("server was called %d times, want exactly %d (bounded, not unbounded)", calls, maxRedirectHops)
	}
}

// TestClient_RedirectLoop_NotRetried proves a bounded redirect-loop failure
// doesn't additionally burn the outer retry budget: doWithRetry must
// surface it on the first attempt, since retrying would just chase the same
// loop again.
func TestClient_RedirectLoop_NotRetried(t *testing.T) {
	deterministicBackoff(t)
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Location", "/loop")
		w.WriteHeader(http.StatusMovedPermanently)
	}))
	defer srv.Close()

	c := newTestClient(srv, 3) // would retry up to 3 additional times if misclassified
	_, err := c.Get(context.Background(), "/loop", nil)
	var loopErr *RedirectLoopError
	if !errors.As(err, &loopErr) {
		t.Fatalf("error = %T (%v), want *RedirectLoopError", err, err)
	}
	if calls != maxRedirectHops {
		t.Errorf("server was called %d times, want exactly %d (the outer retry budget must not add more)", calls, maxRedirectHops)
	}
}

// TestClient_DebugTracesBothHops confirms --debug still traces each hop of
// an allowed (same-path) redirect individually — not just one aggregate
// line for the whole chain — by constructing the same tripper ordering
// New() builds (redirectTripper -> debugTripper -> ... ) and asserting the
// captured trace has a request/response line pair for each hop.
func TestClient_DebugTracesBothHops(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer target.Close()

	initial := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL+"/x")
		w.WriteHeader(http.StatusMovedPermanently)
	}))
	defer initial.Close()

	var buf bytes.Buffer
	hc := initial.Client()
	hc.Transport = &redirectTripper{next: &debugTripper{next: hc.Transport, out: &buf}}
	c := &Client{httpClient: hc, baseURL: initial.URL, maxRetries: 0}

	_, err := c.Get(context.Background(), "/x", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	trace := buf.String()
	if n := strings.Count(trace, "> GET "); n != 2 {
		t.Errorf("saw %d request trace lines, want 2 (one per hop):\n%s", n, trace)
	}
	if n := strings.Count(trace, "< GET "); n != 2 {
		t.Errorf("saw %d response trace lines, want 2 (one per hop):\n%s", n, trace)
	}
	if !strings.Contains(trace, initial.URL+"/x") {
		t.Errorf("trace missing the first hop's URL:\n%s", trace)
	}
	if !strings.Contains(trace, target.URL+"/x") {
		t.Errorf("trace missing the second hop's URL:\n%s", trace)
	}
}

// TestClient_IDPathRedirectChainRefused_StillRefused re-confirms the live
// regression this whole guard exists for: `users get "/"` escapes to
// .../users/%2F, which 301s to a trailing-slash path (a different, changed
// path) and then to the bare collection. Both hops change the path, so the
// narrowed guard must still refuse at the first one.
func TestClient_IDPathRedirectChainRefused_StillRefused(t *testing.T) {
	var collectionCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/api/v1/users/%2F":
			w.Header().Set("Location", "/api/v1/users/")
			w.WriteHeader(http.StatusMovedPermanently)
		case "/api/v1/users/":
			w.Header().Set("Location", "/api/v1/users")
			w.WriteHeader(http.StatusMovedPermanently)
		case "/api/v1/users":
			collectionCalled = true
			_, _ = w.Write([]byte(`{"list":[{"id":"1"},{"id":"2"}]}`))
		default:
			t.Fatalf("unexpected request to %s", r.URL.EscapedPath())
		}
	}))
	defer srv.Close()

	c := newTestClient(srv, 0)
	_, err := c.Get(context.Background(), Path("/api/v1/users/%s", "/"), nil)
	var redirErr *RedirectError
	if !errors.As(err, &redirErr) {
		t.Fatalf(`error = %T (%v), want *RedirectError`, err, err)
	}
	if collectionCalled {
		t.Error("the collection endpoint was reached; the narrowed guard must still refuse this chain")
	}
}

// TestResolveSamePath unit-tests the path-comparison decision directly for
// cases that are impractical to stand up as a real network hop (notably,
// resolving a Location against a base whose own URL was itself already
// parsed), as a supplement to the httptest-driven tests above, not a
// replacement for them.
func TestResolveSamePath(t *testing.T) {
	base, err := url.Parse("http://example.test/x/%2F")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		location string
		wantOK   bool
	}{
		{"empty location never resolves", "", false},
		{"identical absolute URL", "http://example.test/x/%2F", true},
		{"trailing slash added counts as changed", "http://example.test/x/%2F/", false},
		{"unparseable location", "http://ex ample.test", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := resolveSamePath(base, tc.location)
			if ok != tc.wantOK {
				t.Errorf("resolveSamePath(%q) ok = %v, want %v", tc.location, ok, tc.wantOK)
			}
		})
	}
}
