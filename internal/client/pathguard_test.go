package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestDo_EmptyPathSegmentNeverSendsRequest proves the guard fires before any
// network I/O: the handler fails the test if it is ever invoked. The guard
// itself (pathHasBadSegment) lives in and is unit-tested by
// internal/transport; this proves it's actually wired into Client.do.
func TestDo_EmptyPathSegmentNeverSendsRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("server must not be called for a request with an empty path segment, got %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	c := newTestClient(srv, 0)

	_, err := c.Get(context.Background(), "/api/v1/policies/", nil)
	if err == nil {
		t.Fatal("Get with trailing-empty-segment path: expected an error, got nil")
	}
	var pathErr *PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("Get error = %T (%v), want *PathError", err, err)
	}

	_, err = c.Delete(context.Background(), "/api/v1/policies//sub")
	if err == nil {
		t.Fatal("Delete with interior-empty-segment path: expected an error, got nil")
	}
	if !errors.As(err, &pathErr) {
		t.Fatalf("Delete error = %T (%v), want *PathError", err, err)
	}
}

// TestDo_LegitimatePathStillSendsRequest guards against an over-eager
// detector: a well-formed path must still reach the server.
func TestDo_LegitimatePathStillSendsRequest(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestClient(srv, 0)
	if _, err := c.Get(context.Background(), "/api/v1/policies/abc123", nil); err != nil {
		t.Fatalf("Get: unexpected error: %v", err)
	}
	if !called {
		t.Fatal("server was never called for a well-formed path")
	}
}

// TestDo_EscapedSlashInIDStillSendsRequest proves the guard reads
// req.URL.EscapedPath(), not the decoded req.URL.Path. client.Path
// percent-escapes an id, so an id ending in "/" becomes a literal "%2F" in
// the escaped path (no trailing empty segment) but decodes to a trailing "/"
// (a false empty-segment positive) if the guard ever read the decoded path
// instead. This drives a real request built via client.Path end-to-end
// through an httptest.Server so the assertion is on wire behavior, not the
// pure pathHasBadSegment helper.
func TestDo_EscapedSlashInIDStillSendsRequest(t *testing.T) {
	var called bool
	var gotEscapedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		gotEscapedPath = r.URL.EscapedPath()
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestClient(srv, 0)
	path := Path("/api/v1/apps/%s", "abc/") // escapes to ".../abc%2F"; decodes to ".../abc/"
	if _, err := c.Get(context.Background(), path, nil); err != nil {
		t.Fatalf("Get with an id containing an escaped slash: unexpected error: %v", err)
	}
	if !called {
		t.Fatal("server was never called for a legitimate id containing an escaped slash")
	}
	if gotEscapedPath != "/api/v1/apps/abc%2F" {
		t.Fatalf("server saw escaped path %q, want /api/v1/apps/abc%%2F", gotEscapedPath)
	}
}

// TestDo_DotSegmentNeverSendsRequest drives client.Path end-to-end: "." and
// ".." survive url.PathEscape unchanged, and the server normalizes them away
// for any method, so every method must be refused before the wire.
func TestDo_DotSegmentNeverSendsRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("request sent: %s %s", r.Method, r.URL.EscapedPath())
	}))
	defer srv.Close()

	c := newTestClient(srv, 0)
	ctx := context.Background()
	for _, id := range []string{".", ".."} {
		calls := map[string]func() error{
			"GET":    func() error { _, err := c.Get(ctx, Path("/api/v1/users/%s", id), nil); return err },
			"DELETE": func() error { _, err := c.Delete(ctx, Path("/api/v1/users/%s", id)); return err },
			"POST": func() error {
				_, err := c.Post(ctx, Path("/api/v1/webhooks/%s/test", id), map[string]any{})
				return err
			},
			"PATCH": func() error { _, err := c.Patch(ctx, Path("/api/v1/users/%s", id), map[string]any{}); return err },
		}
		for method, call := range calls {
			var pathErr *PathError
			if err := call(); !errors.As(err, &pathErr) {
				t.Errorf("%s id %q: error = %T (%v), want *PathError", method, id, err, err)
			}
		}
	}
}

// TestDo_DottedIDStillSendsRequest guards against over-matching: an id that
// merely contains dots is a real id.
func TestDo_DottedIDStillSendsRequest(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.EscapedPath())
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newTestClient(srv, 0)
	ids := []string{"a.b", "..a", "v1.2", "..."}
	for _, id := range ids {
		if _, err := c.Get(context.Background(), Path("/api/v1/users/%s", id), nil); err != nil {
			t.Errorf("id %q: unexpected error: %v", id, err)
		}
	}
	if len(got) != len(ids) {
		t.Fatalf("server saw %d requests (%v), want %d", len(got), got, len(ids))
	}
}
