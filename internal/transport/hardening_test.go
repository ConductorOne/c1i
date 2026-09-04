package transport

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestResolveAllowedRedirect_Scheme covers the scheme rules for a same-path,
// same-host redirect: an https->http downgrade is refused, while an
// http->https upgrade and a same-scheme hop are allowed.
func TestResolveAllowedRedirect_Scheme(t *testing.T) {
	cases := []struct {
		name   string
		base   string
		loc    string
		wantOK bool
	}{
		{"https->http downgrade refused", "https://dist.example/x", "http://dist.example/x", false},
		{"http->https upgrade allowed", "http://dist.example/x", "https://dist.example/x", true},
		{"https->https same-path allowed", "https://dist.example/x", "https://dist.example/x?q=1", true},
		{"scheme-relative keeps https allowed", "https://dist.example/x", "//dist.example/x", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base, err := url.Parse(tc.base)
			if err != nil {
				t.Fatal(err)
			}
			_, ok := resolveAllowedRedirect(base, tc.loc)
			if ok != tc.wantOK {
				t.Errorf("resolveAllowedRedirect(%q, %q) ok = %v, want %v", tc.base, tc.loc, ok, tc.wantOK)
			}
		})
	}
}

// TestClient_RefusesSchemeDowngradeRedirect drives a real server that (over
// http, standing in for the request scheme) issues an absolute https->http
// same-path redirect; the client must refuse it as *RedirectError rather than
// follow a downgrade. The request scheme is rewritten to https by the same
// scheme-swap trick used elsewhere so the guard sees an https base.
func TestClient_RefusesSchemeDowngradeRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/x/abc" {
			t.Fatalf("unexpected request to %s", r.URL.EscapedPath())
		}
		// Point Location at an explicit http URL at the same host:path. From an
		// https base that's a downgrade the guard must refuse.
		u := *r.URL
		u.Scheme = "http"
		u.Host = r.Host
		w.Header().Set("Location", u.String())
		w.WriteHeader(http.StatusMovedPermanently)
	}))
	defer srv.Close()

	// httpsForcingTransport makes the redirectTripper's base request look like
	// https so an http Location is a downgrade; it dials the real (http) server.
	hc := &http.Client{Transport: &redirectTripper{next: &httpsForcingTransport{next: http.DefaultTransport}}}
	c := &Client{httpClient: hc, maxRetries: 0}

	req, err := http.NewRequest(http.MethodGet, strings.Replace(srv.URL, "http://", "https://", 1)+"/x/abc", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Do(req)
	var redirErr *RedirectError
	if !errors.As(err, &redirErr) {
		t.Fatalf("error = %T (%v), want *RedirectError for an https->http downgrade", err, err)
	}
}

// httpsForcingTransport dials over plain http (the httptest server) while
// leaving the request URL's https scheme intact for the guard to inspect.
type httpsForcingTransport struct{ next http.RoundTripper }

func (h *httpsForcingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	dialURL := *req.URL
	dialURL.Scheme = "http"
	clone := req.Clone(req.Context())
	clone.URL = &dialURL
	return h.next.RoundTrip(clone)
}

// TestWithMaxResponseBytes bounds the body read: a response larger than the cap
// fails, one at or under it succeeds. Default (unset) stays unbounded.
func TestWithMaxResponseBytes(t *testing.T) {
	const limit = 16
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := 8
		if r.URL.Query().Get("big") != "" {
			n = 1024
		}
		_, _ = w.Write([]byte(strings.Repeat("a", n)))
	}))
	defer srv.Close()

	bounded := New(srv.Client().Transport, WithMaxRetries(0), WithMaxResponseBytes(limit))
	// Under the cap: OK.
	resp, err := get(t, bounded, srv.URL+"/small")
	if err != nil {
		t.Fatalf("under-cap read errored: %v", err)
	}
	if len(resp.Body) != 8 {
		t.Errorf("body len = %d, want 8", len(resp.Body))
	}
	// Over the limit: error, no body.
	if _, err := get(t, bounded, srv.URL+"/big?big=1"); err == nil {
		t.Fatal("over-cap read did not error")
	}

	// Exactly at the cap: OK.
	atCap := New(srv.Client().Transport, WithMaxRetries(0), WithMaxResponseBytes(8))
	if _, err := get(t, atCap, srv.URL+"/small"); err != nil {
		t.Fatalf("at-cap read errored: %v", err)
	}

	// Unbounded default: a large body is fine.
	unbounded := New(srv.Client().Transport, WithMaxRetries(0))
	if r, err := get(t, unbounded, srv.URL+"/big?big=1"); err != nil || len(r.Body) != 1024 {
		t.Fatalf("unbounded read = (%d bytes, %v), want (1024, nil)", len(r.Body), err)
	}
}
