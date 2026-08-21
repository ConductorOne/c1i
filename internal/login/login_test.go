package login

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ConductorOne/c1i/internal/client"
)

// TestStartDeviceFlowClassifiesHTTPError verifies a non-2xx from the device
// endpoint surfaces as a *client.APIError (carrying the status), so cmd/errors.go
// maps it to the exit-code taxonomy instead of collapsing to a generic exit 1.
func TestStartDeviceFlowClassifiesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("upstream down"))
	}))
	defer srv.Close()

	_, err := StartDeviceFlow(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected an error from a 503 device endpoint")
	}
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v (%T) does not unwrap to *client.APIError", err, err)
	}
	if apiErr.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("StatusCode = %d, want 503", apiErr.StatusCode)
	}
}

// TestPollForTokenClassifiesErrors covers both non-2xx paths the token endpoint
// can take: an OAuth error response (access_denied → *client.AuthError, exit 3)
// and an unparseable non-2xx body (→ *client.APIError carrying the status).
func TestPollForTokenClassifiesErrors(t *testing.T) {
	code := &DeviceCode{DeviceCode: "dc", Interval: 1, ExpiresIn: 60}

	t.Run("oauth error is an auth failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"access_denied","error_description":"user denied the request"}`))
		}))
		defer srv.Close()

		_, err := PollForToken(context.Background(), srv.URL, code)
		var authErr *client.AuthError
		if !errors.As(err, &authErr) {
			t.Fatalf("error %v (%T) does not unwrap to *client.AuthError", err, err)
		}
	})

	t.Run("unparseable body carries the status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("not json"))
		}))
		defer srv.Close()

		_, err := PollForToken(context.Background(), srv.URL, code)
		var apiErr *client.APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("error %v (%T) does not unwrap to *client.APIError", err, err)
		}
		if apiErr.StatusCode != http.StatusInternalServerError {
			t.Errorf("StatusCode = %d, want 500", apiErr.StatusCode)
		}
	})
}

// TestCreatePersonalClientClassifiesHTTPError covers the third remote call in
// the device flow. The device and token endpoints were already covered; this
// one was not, so nothing pinned it inside the exit-code taxonomy.
func TestCreatePersonalClientClassifiesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("gateway blew up"))
	}))
	defer srv.Close()

	_, err := createPersonalClient(context.Background(), srv.URL, "tok")
	if err == nil {
		t.Fatal("expected an error from a 502 personal-client endpoint")
	}
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v (%T) does not unwrap to *client.APIError", err, err)
	}
	if apiErr.StatusCode != http.StatusBadGateway {
		t.Errorf("StatusCode = %d, want 502", apiErr.StatusCode)
	}
}

// TestPollForTokenServerFailureIsNotAnAuthFailure guards the misclassification
// a reviewer found: the OAuth error branch was entered whenever the body merely
// unmarshalled into {error,error_description}, so a 5xx returning unrelated
// JSON (no "error" key) left Error empty, fell to the default branch, and came
// back as *client.AuthError — reporting a server outage as an auth failure
// (exit 3 instead of exit 6). A 5xx must stay on the transport taxonomy whether
// or not its body happens to be OAuth-shaped.
func TestPollForTokenServerFailureIsNotAnAuthFailure(t *testing.T) {
	code := &DeviceCode{DeviceCode: "dc", Interval: 1, ExpiresIn: 60}

	for _, tc := range []struct {
		name string
		body string
	}{
		{"json without an error key", `{"message":"internal failure","trace":"abc123"}`},
		{"json with an oauth-shaped error", `{"error":"server_error","error_description":"backend unavailable"}`},
		{"empty json object", `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			_, err := PollForToken(context.Background(), srv.URL, code)
			var authErr *client.AuthError
			if errors.As(err, &authErr) {
				t.Fatalf("a 500 was classified as an auth failure: %v", err)
			}
			var apiErr *client.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error %v (%T) does not unwrap to *client.APIError", err, err)
			}
			if apiErr.StatusCode != http.StatusInternalServerError {
				t.Errorf("StatusCode = %d, want 500", apiErr.StatusCode)
			}
		})
	}
}

// TestAPIErrorPathIsRequestPath pins that login's APIError carries the request
// PATH, not the full base URL. internal/client sets Path: req.URL.Path, and
// --error-format json surfaces it, so a full URL here would render
// inconsistently with every other API failure in the CLI.
func TestAPIErrorPathIsRequestPath(t *testing.T) {
	fail := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("down"))
	}

	for _, tc := range []struct {
		name     string
		call     func(base string) error
		wantPath string
	}{
		{"device", func(base string) error { _, err := StartDeviceFlow(context.Background(), base); return err },
			"/auth/v1/device_authorization"},
		{"token", func(base string) error {
			_, err := PollForToken(context.Background(), base, &DeviceCode{DeviceCode: "dc", Interval: 1, ExpiresIn: 60})
			return err
		}, "/auth/v1/token"},
		{"personal client", func(base string) error { _, err := createPersonalClient(context.Background(), base, "tok"); return err },
			"/api/v1/iam/personal_clients"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(fail))
			defer srv.Close()

			err := tc.call(srv.URL)
			var apiErr *client.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error %v (%T) does not unwrap to *client.APIError", err, err)
			}
			if apiErr.Path != tc.wantPath {
				t.Errorf("Path = %q, want %q (the request path, not the full URL)", apiErr.Path, tc.wantPath)
			}
		})
	}
}

// TestPollForTokenFiveHundredWithPendingBodyIsAServerError closes a gap found
// by review of the fix above. The authorization_pending/slow_down cases mean
// "keep polling", so if they are evaluated before the 5xx check, a 500 whose
// body happens to carry one of them keeps polling through a real outage and
// then reports "device code expired" as a bare error (exit 1) instead of a
// server failure (exit 6) — a misleading answer for an operator, and it
// contradicted the guarantee that a 5xx always stays on the transport
// taxonomy. RFC 8628 has the server return both codes with HTTP 400, so a 5xx
// carrying one is a malfunctioning server, not a grant in progress.
func TestPollForTokenFiveHundredWithPendingBodyIsAServerError(t *testing.T) {
	for _, oauthCode := range []string{"authorization_pending", "slow_down"} {
		t.Run(oauthCode, func(t *testing.T) {
			var requests int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"` + oauthCode + `"}`))
			}))
			defer srv.Close()

			// ExpiresIn is generous: if the 5xx were (wrongly) treated as
			// "keep polling", this would burn the full window and come back
			// with "device code expired" instead of failing fast.
			code := &DeviceCode{DeviceCode: "dc", Interval: 1, ExpiresIn: 30}
			_, err := PollForToken(context.Background(), srv.URL, code)
			if err == nil {
				t.Fatal("expected an error from a 500 token endpoint")
			}
			var apiErr *client.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error %v (%T) does not unwrap to *client.APIError; a 5xx must stay on the transport taxonomy", err, err)
			}
			if apiErr.StatusCode != http.StatusInternalServerError {
				t.Errorf("StatusCode = %d, want 500", apiErr.StatusCode)
			}
			if requests != 1 {
				t.Errorf("made %d requests, want 1 — a 5xx should fail fast, not keep polling", requests)
			}
		})
	}
}

// TestStartDeviceFlowTimesOut proves a hung device-authorization endpoint
// doesn't hang c1i auth login forever: before this package used
// internal/transport, every request here went through http.DefaultClient,
// which has no Timeout at all, so a connection that never answers would
// block indefinitely. requestTimeout is overridden here (it's a var, not a
// const, for exactly this) so the test doesn't have to wait out the real 30s.
func TestStartDeviceFlowTimesOut(t *testing.T) {
	orig := requestTimeout
	requestTimeout = 50 * time.Millisecond
	t.Cleanup(func() { requestTimeout = orig })

	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	defer srv.Close()

	done := make(chan error, 1)
	go func() {
		_, err := StartDeviceFlow(context.Background(), srv.URL)
		done <- err
	}()

	// Unblock the handler (so the deferred srv.Close() above doesn't itself
	// hang waiting for the still-active connection) before asserting, not via
	// another deferred call: defers run LIFO, and registering this after
	// srv.Close() would run it AFTER Close already started waiting.
	defer close(block)

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a timeout error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("StartDeviceFlow did not return within 5s -- a hung device-authorization endpoint must time out, not hang")
	}
}

// TestDeviceFlowSendsUserAgent proves every request in the device flow
// carries the shared transport's user-agent — before this package used
// internal/transport, these requests went through http.DefaultClient and
// sent Go's default User-Agent instead.
func TestDeviceFlowSendsUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, _ = StartDeviceFlow(context.Background(), srv.URL)
	if !strings.HasPrefix(gotUA, "c1.ai/c1i") {
		t.Errorf("device_authorization User-Agent = %q, want it to start with c1.ai/c1i", gotUA)
	}

	_, _ = createPersonalClient(context.Background(), srv.URL, "tok")
	if !strings.HasPrefix(gotUA, "c1.ai/c1i") {
		t.Errorf("personal_clients User-Agent = %q, want it to start with c1.ai/c1i", gotUA)
	}
}
