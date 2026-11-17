package login

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPollForTokenRedactsErrorDescription(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_request","error_description":"bad /oauth/token?client_secret=SEKL1&keep=1"}`))
	}))
	defer srv.Close()
	_, err := PollForToken(context.Background(), srv.URL, &DeviceCode{DeviceCode: "dc", Interval: 1, ExpiresIn: 60})
	if err == nil || strings.Contains(err.Error(), "SEKL1") || !strings.Contains(err.Error(), "keep=1") {
		t.Errorf("err = %v", err)
	}
}
