package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestClient_RefusesRedirect is table-driven over every 3xx status net/http's
// own Client would otherwise follow, plus a 200 control case proving the
// guard doesn't touch a normal response. It drives a real httptest.Server so
// the assertion is on wire behavior (the redirect target must never be
// called), not a helper in isolation.
func TestClient_RefusesRedirect(t *testing.T) {
	redirectStatusesToTest := []int{
		http.StatusMovedPermanently,  // 301
		http.StatusFound,             // 302
		http.StatusTemporaryRedirect, // 307
		http.StatusPermanentRedirect, // 308
	}
	for _, status := range redirectStatusesToTest {
		t.Run(fmt.Sprintf("%d", status), func(t *testing.T) {
			var targetCalled bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.EscapedPath() {
				case "/redirect":
					w.Header().Set("Location", "/target")
					w.WriteHeader(status)
				case "/target":
					targetCalled = true
					_, _ = w.Write([]byte(`{"list":["should","never","be","seen"]}`))
				default:
					t.Fatalf("unexpected request to %s", r.URL.EscapedPath())
				}
			}))
			defer srv.Close()

			c := newTestClient(srv, 0)
			_, err := c.Get(context.Background(), "/redirect", nil)
			if err == nil {
				t.Fatalf("status %d: expected an error, got nil", status)
			}
			var redirErr *RedirectError
			if !errors.As(err, &redirErr) {
				t.Fatalf("status %d: error = %T (%v), want *RedirectError", status, err, err)
			}
			if redirErr.StatusCode != status {
				t.Errorf("status %d: RedirectError.StatusCode = %d, want %d", status, redirErr.StatusCode, status)
			}
			if redirErr.Location != "/target" {
				t.Errorf("status %d: RedirectError.Location = %q, want %q", status, redirErr.Location, "/target")
			}
			if targetCalled {
				t.Errorf("status %d: redirect target was called; the client must refuse the redirect, not follow it", status)
			}
		})
	}

	t.Run("200 passes through untouched", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer srv.Close()

		c := newTestClient(srv, 0)
		body, err := c.Get(context.Background(), "/x", nil)
		if err != nil {
			t.Fatalf("unexpected error for a plain 200: %v", err)
		}
		if string(body) != `{"ok":true}` {
			t.Errorf("body = %s, want {\"ok\":true}", body)
		}
	})
}

// TestClient_IDPathRedirectChainRefused reproduces the live defect this
// guards against: `users get "/"` escapes to .../users/%2F, which the real
// API 301s to .../users/ (a trailing-empty-segment path — exactly the shape
// client.PathError already refuses), which itself 301s to .../users (the
// bare collection, HTTP 200). PathError's guard only inspects the request
// this CLI constructs; it can't see a redirect target the *server* produces,
// so without this fix the second hop reaches the collection endpoint and
// returns it with exit 0. The client must refuse at the very first redirect
// and the collection endpoint must never be reached.
func TestClient_IDPathRedirectChainRefused(t *testing.T) {
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
	if err == nil {
		t.Fatal(`expected an error for id "/", got nil`)
	}
	var redirErr *RedirectError
	if !errors.As(err, &redirErr) {
		t.Fatalf(`error = %T (%v), want *RedirectError`, err, err)
	}
	if collectionCalled {
		t.Error("the collection endpoint was reached; the client followed the redirect chain instead of refusing it")
	}
}

// TestClient_DotIDPathRedirectRefused reproduces the live defect for the
// other id shape that normalizes to the collection: `users get "."` escapes
// to .../users/. (url.PathEscape leaves "." unescaped, unlike "/" which
// becomes %2F for the "/" case above), and the real API collapses that
// single dot segment server-side, 301-ing directly to .../users — one hop,
// not the two-hop chain the "/" case goes through. Same guard, same refusal
// branch (target path != request path), different id and a shorter path to
// it; see resolveAllowedRedirect in client.go.
func TestClient_DotIDPathRedirectRefused(t *testing.T) {
	var collectionCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/api/v1/users/.":
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
	_, err := c.Get(context.Background(), Path("/api/v1/users/%s", "."), nil)
	if err == nil {
		t.Fatal(`expected an error for id ".", got nil`)
	}
	var redirErr *RedirectError
	if !errors.As(err, &redirErr) {
		t.Fatalf(`error = %T (%v), want *RedirectError`, err, err)
	}
	if collectionCalled {
		t.Error("the collection endpoint was reached; the client followed the redirect instead of refusing it")
	}
}

// TestClient_RedirectNotRetried proves a redirect is surfaced immediately,
// not treated as a transient failure and retried: retrying a permanent
// redirect wastes the backoff budget on something that will never resolve
// differently, and the escaped-path shape means the same request would keep
// redirecting identically on every attempt.
func TestClient_RedirectNotRetried(t *testing.T) {
	deterministicBackoff(t)
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Location", "/target")
		w.WriteHeader(http.StatusMovedPermanently)
	}))
	defer srv.Close()

	c := newTestClient(srv, 3) // would retry up to 3 additional times if misclassified
	_, err := c.Get(context.Background(), "/redirect", nil)
	var redirErr *RedirectError
	if !errors.As(err, &redirErr) {
		t.Fatalf("error = %T (%v), want *RedirectError", err, err)
	}
	if calls != 1 {
		t.Errorf("server was called %d times, want 1 (a redirect must not be retried)", calls)
	}
}
