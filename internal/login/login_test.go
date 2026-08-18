package login

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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
