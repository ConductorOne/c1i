package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	// Also pinned to a hard-coded literal, not maxRedirectHops itself: the
	// assertion above compares against the same constant the production code
	// uses, so it can't catch an accidental change to maxRedirectHops (e.g.
	// raising it far higher) -- only a literal does. Update this literal
	// deliberately if maxRedirectHops itself changes.
	if calls != 5 {
		t.Errorf("server was called %d times, want exactly 5", calls)
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
	// See the matching comment in TestClient_RedirectLoop_SamePath_Bounded:
	// pinned to a literal so a change to maxRedirectHops itself can't hide
	// behind this assertion comparing against its own symbol.
	if calls != 5 {
		t.Errorf("server was called %d times, want exactly 5", calls)
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

// TestResolveAllowedRedirect unit-tests the path-and-host decision directly
// for cases that are impractical to stand up as a real network hop (notably,
// resolving a Location against a base whose own URL was itself already
// parsed), as a supplement to the httptest-driven tests above, not a
// replacement for them.
func TestResolveAllowedRedirect(t *testing.T) {
	base, err := url.Parse("http://api.tenant.example/x/%2F")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		location string
		wantOK   bool
	}{
		{"empty location never resolves", "", false},
		{"identical absolute URL", "http://api.tenant.example/x/%2F", true},
		{"trailing slash added counts as changed", "http://api.tenant.example/x/%2F/", false},
		{"unparseable location", "http://ex ample.test", false},
		{"same path, subdomain-related host: allowed", "http://tenant.example/x/%2F", true},
		{"same path, unrelated host: refused", "http://evil.example/x/%2F", false},
		// base's EscapedPath is "/x/%2F" (one segment: an id containing a
		// literal slash), decoded Path is "/x//". A target whose literal
		// path is "/x//" has the SAME decoded Path but a DIFFERENT escaped
		// path (no percent-encoding at all) -- a different resource (two
		// segments, the second empty) that a decoded-path comparison would
		// wrongly call unchanged. Must stay refused.
		{"escaped separator vs. literal separator (decoded paths coincide): refused", "/x//", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := resolveAllowedRedirect(base, tc.location)
			if ok != tc.wantOK {
				t.Errorf("resolveAllowedRedirect(%q) ok = %v, want %v", tc.location, ok, tc.wantOK)
			}
		})
	}
}

// TestHostInScope guards the label-boundary safety of the suffix check
// directly: a naive substring/suffix match without the "." would let
// "eviltenant.example" pass as a subdomain of "tenant.example".
func TestHostInScope(t *testing.T) {
	cases := []struct {
		name string
		base string
		tgt  string
		want bool
	}{
		{"identical host", "tenant.example", "tenant.example", true},
		{"identical host, case-insensitive", "Tenant.Example", "tenant.example", true},
		{"subdomain of base", "tenant.example", "api.tenant.example", true},
		{"base is subdomain of target", "api.tenant.example", "tenant.example", true},
		{"unrelated host, shared suffix label boundary violated", "tenant.example", "eviltenant.example", false},
		{"unrelated host, shared suffix label boundary violated, reversed", "eviltenant.example", "tenant.example", false},
		{"completely unrelated host", "tenant.example", "evil.example", false},
		// A single-label target is never a real canonicalization of a public
		// tenant host (every real domain family here has a two-label apex),
		// so the prefix branch has a floor: the target must have >= 2 labels.
		{"single-label target below the floor: refused", "tenant.example", "example", false},
		// The identical-host branch is not subject to the floor: a bare
		// single-label host is a legitimate identical-host (local dev) target.
		{"identical single-label host (localhost): allowed", "localhost", "localhost", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := &url.URL{Host: tc.base}
			tgt := &url.URL{Host: tc.tgt}
			if got := hostInScope(base, tgt); got != tc.want {
				t.Errorf("hostInScope(%q, %q) = %v, want %v", tc.base, tc.tgt, got, tc.want)
			}
		})
	}
}

// authInjectingTripper stands in for the oauth2 transport in production:
// it attaches a bearer token to every request it forwards, with no
// awareness of host. It exists so these tests can observe, on the wire,
// exactly what the coordinator's finding was about — whether that token
// reaches a redirect target — rather than inferring it from the error type
// alone.
type authInjectingTripper struct {
	next  http.RoundTripper
	token string
}

func (a *authInjectingTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+a.token)
	return a.next.RoundTrip(req)
}

// hostRewritingTransport lets a test address fake hostnames (e.g.
// "tenant.example", "evil.example") that don't resolve, by mapping each to
// the real httptest.Server address to dial while preserving the fake name
// as the wire Host header (so a handler can key off r.Host, and so
// redirectTripper — which runs above this transport and reads req.URL as
// given — makes its allow/refuse decision against the fake hostnames, not
// the real loopback addresses). It never mutates the caller's *http.Request
// in place, only a clone, since redirectTripper still holds and reuses the
// original.
type hostRewritingTransport struct {
	next  *http.Transport
	hosts map[string]string // fake hostname -> real "127.0.0.1:port"
}

func (h *hostRewritingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	fakeHost := req.URL.Hostname()
	real, ok := h.hosts[fakeHost]
	if !ok {
		return nil, fmt.Errorf("test hostRewritingTransport: no real address mapped for host %q", fakeHost)
	}
	clone := req.Clone(req.Context())
	clone.Host = req.URL.Host // preserve the fake name as the wire Host header
	u := *req.URL
	u.Host = real
	clone.URL = &u
	return h.next.RoundTrip(clone)
}

// TestClient_RedirectSubdomainHost_SamePath_Followed proves a same-path
// redirect to a different but same-scope host (api.<domain> <-> <domain>,
// in both directions) is followed and still carries auth — that's the
// intended, safe case hostInScope exists to keep working.
func TestClient_RedirectSubdomainHost_SamePath_Followed(t *testing.T) {
	const token = "SECRET-TOKEN"

	run := func(t *testing.T, fromHost, toHost string) {
		var toHostCalled bool
		var gotAuth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Host {
			case fromHost:
				w.Header().Set("Location", "http://"+toHost+"/x")
				w.WriteHeader(http.StatusMovedPermanently)
			case toHost:
				toHostCalled = true
				gotAuth = r.Header.Get("Authorization")
				_, _ = w.Write([]byte(`{"ok":true}`))
			default:
				t.Fatalf("unexpected Host %q", r.Host)
			}
		}))
		defer srv.Close()

		realAddr := srv.Listener.Addr().String()
		hc := &http.Client{Transport: &redirectTripper{next: &authInjectingTripper{
			token: token,
			next: &hostRewritingTransport{
				next:  http.DefaultTransport.(*http.Transport),
				hosts: map[string]string{fromHost: realAddr, toHost: realAddr},
			},
		}}}
		c := &Client{httpClient: hc, baseURL: "http://" + fromHost, maxRetries: 0}

		body, err := c.Get(context.Background(), "/x", nil)
		if err != nil {
			t.Fatalf("unexpected error following %s -> %s: %v", fromHost, toHost, err)
		}
		if string(body) != `{"ok":true}` {
			t.Errorf("body = %s, want the target's body", body)
		}
		if !toHostCalled {
			t.Fatalf("%s was never contacted; the same-scope redirect must be followed", toHost)
		}
		if gotAuth != "Bearer "+token {
			t.Errorf("%s saw Authorization = %q, want %q (a followed in-scope hop is meant to carry auth)", toHost, gotAuth, "Bearer "+token)
		}
	}

	t.Run("api.tenant.example -> tenant.example", func(t *testing.T) {
		run(t, "api.tenant.example", "tenant.example")
	})
	t.Run("tenant.example -> api.tenant.example", func(t *testing.T) {
		run(t, "tenant.example", "api.tenant.example")
	})
}

// TestClient_RedirectUnrelatedHost_Refused is the coordinator's finding,
// closed: a same-path redirect to a host outside the request host's trust
// scope must be refused, and — the point of the fix — the unrelated host
// must never be contacted at all, so it never has the chance to see the
// bearer token attached by the transport beneath redirectTripper.
func TestClient_RedirectUnrelatedHost_Refused(t *testing.T) {
	const token = "SECRET-TOKEN"
	const goodHost = "tenant.example"
	const evilHost = "evil.example"

	var evilCalls int
	var evilSawAuth string
	evilSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		evilCalls++
		evilSawAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"stolen":true}`))
	}))
	defer evilSrv.Close()

	goodSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != goodHost {
			t.Fatalf("unexpected Host %q", r.Host)
		}
		w.Header().Set("Location", "http://"+evilHost+"/x")
		w.WriteHeader(http.StatusMovedPermanently)
	}))
	defer goodSrv.Close()

	hc := &http.Client{Transport: &redirectTripper{next: &authInjectingTripper{
		token: token,
		next: &hostRewritingTransport{
			next: http.DefaultTransport.(*http.Transport),
			hosts: map[string]string{
				goodHost: goodSrv.Listener.Addr().String(),
				evilHost: evilSrv.Listener.Addr().String(),
			},
		},
	}}}
	c := &Client{httpClient: hc, baseURL: "http://" + goodHost, maxRetries: 0}

	body, err := c.Get(context.Background(), "/x", nil)
	var redirErr *RedirectError
	if !errors.As(err, &redirErr) {
		t.Fatalf("error = %T (%v), want *RedirectError", err, err)
	}
	if body != nil {
		t.Errorf("body = %q, want nil (no body on refusal)", body)
	}
	if evilCalls != 0 {
		t.Fatalf("%s was contacted %d time(s); an out-of-scope redirect must never be followed", evilHost, evilCalls)
	}
	if evilSawAuth != "" {
		t.Errorf("%s observed Authorization = %q; it must never see any request, let alone the token", evilHost, evilSawAuth)
	}
}

// TestClient_RedirectSingleLabelTarget_Refused covers the floor on the
// prefix-relationship branch: a same-path redirect from a real tenant host
// down to a bare single-label host must be refused even though it fits the
// "<label>." prefix pattern, since a single-label target is never a real
// canonicalization of a public tenant host — and the target must never be
// contacted at all.
func TestClient_RedirectSingleLabelTarget_Refused(t *testing.T) {
	const token = "SECRET-TOKEN"
	const goodHost = "tenant.example"
	const bareHost = "example"

	var bareCalls int
	bareSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bareCalls++
		_, _ = w.Write([]byte(`{"stolen":true}`))
	}))
	defer bareSrv.Close()

	goodSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != goodHost {
			t.Fatalf("unexpected Host %q", r.Host)
		}
		w.Header().Set("Location", "http://"+bareHost+"/x")
		w.WriteHeader(http.StatusMovedPermanently)
	}))
	defer goodSrv.Close()

	hc := &http.Client{Transport: &redirectTripper{next: &authInjectingTripper{
		token: token,
		next: &hostRewritingTransport{
			next: http.DefaultTransport.(*http.Transport),
			hosts: map[string]string{
				goodHost: goodSrv.Listener.Addr().String(),
				bareHost: bareSrv.Listener.Addr().String(),
			},
		},
	}}}
	c := &Client{httpClient: hc, baseURL: "http://" + goodHost, maxRetries: 0}

	_, err := c.Get(context.Background(), "/x", nil)
	var redirErr *RedirectError
	if !errors.As(err, &redirErr) {
		t.Fatalf("error = %T (%v), want *RedirectError", err, err)
	}
	if bareCalls != 0 {
		t.Fatalf("%s was contacted %d time(s); a single-label target below the floor must never be followed", bareHost, bareCalls)
	}
}

// TestClient_RedirectFollowedHop_ReplaysBody proves an allowed (same-path,
// in-scope-host) redirect on a body-bearing method (PUT here; POST is the
// same code path) resends the ORIGINAL body on the followed hop, not an
// empty one: the first hop's Body reader is already drained by the time the
// 3xx comes back, so redirectedRequest must re-obtain it from GetBody. This
// matters concretely for this CLI: `policies update` (POST) and `apps
// set-owners` (PUT) would otherwise silently clear what they meant to set
// if a canonicalization redirect ever fired on one.
//
// The transport disables keep-alives deliberately: with a reused persistent
// connection, net/http.Transport can transparently retry a broken write via
// req.GetBody itself, which would mask a missing replay in redirectedRequest
// by fixing it up one layer further down. Forcing a fresh connection for the
// second hop makes sure this test is exercising redirectedRequest's own
// replay, not net/http's unrelated safety net.
func TestClient_RedirectFollowedHop_ReplaysBody(t *testing.T) {
	const payload = `{"owner_ids":["abc123"]}`

	var redirected bool
	var gotMethod string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !redirected {
			redirected = true
			w.Header().Set("Location", "/x") // same path, forces a second hop
			w.WriteHeader(http.StatusMovedPermanently)
			return
		}
		gotMethod = r.Method
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading followed hop's body: %v", err)
		}
		gotBody = b
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	hc := &http.Client{Transport: &redirectTripper{next: &http.Transport{DisableKeepAlives: true}}}
	c := &Client{httpClient: hc, baseURL: srv.URL, maxRetries: 0}

	body, err := c.Put(context.Background(), "/x", json.RawMessage(payload))
	if err != nil {
		t.Fatalf("unexpected error following a same-path redirect on a PUT: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Errorf("body = %s, want the target's body", body)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("followed hop's method = %q, want %q", gotMethod, http.MethodPut)
	}
	if string(gotBody) != payload {
		t.Errorf("followed hop's body = %q, want %q (an empty body here would silently clear what the request meant to set)", gotBody, payload)
	}
}
