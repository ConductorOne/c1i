package transport

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func doMethod(t *testing.T, c *Client, ctx context.Context, method, url string) (*Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c.Do(req)
}

// statusSequence answers the i-th request with codes[i], repeating the last.
func statusSequence(t *testing.T, codes ...int) *httptest.Server {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		i := int(n.Add(1)) - 1
		w.WriteHeader(codes[min(i, len(codes)-1)])
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDo_PriorSent(t *testing.T) {
	cases := []struct {
		name   string
		method string
		codes  []int
		want   bool
	}{
		{"503 then 404 on DELETE", http.MethodDelete, []int{503, 404}, true},
		{"429 then 404", http.MethodPost, []int{429, 404}, true},
		{"single 404", http.MethodDelete, []int{404}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deterministicBackoff(t)
			srv := statusSequence(t, tc.codes...)
			resp, err := doMethod(t, newTestClient(srv, 3), context.Background(), tc.method, srv.URL+"/api/v1/x")
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			if resp.StatusCode != 404 || resp.PriorSent != tc.want {
				t.Errorf("status %d PriorSent %v, want 404 and %v", resp.StatusCode, resp.PriorSent, tc.want)
			}
		})
	}
}

func attemptErr(t *testing.T, err error) *AttemptError {
	t.Helper()
	var ae *AttemptError
	if !errors.As(err, &ae) {
		t.Fatalf("err = %v (%T), want an *AttemptError in the chain", err, err)
	}
	return ae
}

func TestDo_DialErrorIsNotSent(t *testing.T) {
	deterministicBackoff(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	c := New(http.DefaultTransport, WithMaxRetries(1))
	_, err = doMethod(t, c, context.Background(), http.MethodDelete, "http://"+addr+"/api/v1/x")
	ae := attemptErr(t, err)
	if ae.Sent || ae.PriorSent {
		t.Errorf("Sent=%v PriorSent=%v, want both false for a refused dial", ae.Sent, ae.PriorSent)
	}
}

func TestDo_HijackedAfterHeadersIsSent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	t.Cleanup(srv.Close)
	_, err := doMethod(t, newTestClient(srv, 0), context.Background(), http.MethodPost, srv.URL+"/api/v1/x")
	if ae := attemptErr(t, err); !ae.Sent {
		t.Error("Sent = false, want true: the server read the headers before closing")
	}
}

func TestDo_CanceledBeforeDialIsNotSent(t *testing.T) {
	srv := statusSequence(t, 200)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := doMethod(t, newTestClient(srv, 0), ctx, http.MethodPost, srv.URL+"/api/v1/x")
	if ae := attemptErr(t, err); ae.Sent {
		t.Error("Sent = true, want false: the context was canceled before the dial")
	}
}

func TestDo_CanceledWhileServerHangsIsSent(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
	}))
	t.Cleanup(func() { close(release); srv.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := doMethod(t, newTestClient(srv, 0), ctx, http.MethodPost, srv.URL+"/api/v1/x")
	if ae := attemptErr(t, err); !ae.Sent {
		t.Error("Sent = false, want true: the headers were written before the cancel")
	}
}

// A retry that fails to dial after a sent attempt keeps PriorSent.
func TestDo_DialFailureAfterSentAttemptKeepsPriorSent(t *testing.T) {
	deterministicBackoff(t)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		go srv.CloseClientConnections()
	}))
	url := srv.URL
	c := newTestClient(srv, 1)
	// Close the listener after the first response so the retry cannot dial.
	origSleep := sleepFn
	sleepFn = func(ctx context.Context, d time.Duration) error { srv.Close(); return origSleep(ctx, 0) }
	_, err := doMethod(t, c, context.Background(), http.MethodDelete, url+"/api/v1/x")
	ae := attemptErr(t, err)
	if ae.Sent || !ae.PriorSent {
		t.Errorf("Sent=%v PriorSent=%v, want false/true", ae.Sent, ae.PriorSent)
	}
}

// The error message and errors.As on the wrapped type are unchanged.
func TestAttemptErrorIsTransparent(t *testing.T) {
	inner := &RedirectError{Method: "GET", URL: "u", StatusCode: 301, Location: "l"}
	err := error(&AttemptError{Err: inner, Sent: true})
	var re *RedirectError
	if !errors.As(err, &re) || err.Error() != inner.Error() {
		t.Errorf("AttemptError hides its cause: %v", err)
	}
}

func TestCheckPath(t *testing.T) {
	for _, p := range []string{"/api/v1/apps/", "/api/v1/apps/.", "/api/v1/apps/..", "/api/v1/apps/..;x", "/api/v1/apps/%2e"} {
		var pe *PathError
		if err := CheckPath(http.MethodDelete, p); !errors.As(err, &pe) || pe.Path != p || pe.Method != http.MethodDelete {
			t.Errorf("CheckPath(%q) = %v, want a *PathError naming it", p, err)
		}
	}
	if err := CheckPath(http.MethodDelete, "/api/v1/apps/a"); err != nil {
		t.Errorf("CheckPath on a good path = %v", err)
	}
}
