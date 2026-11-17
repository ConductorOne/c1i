package transport

import (
	"strings"
	"testing"
)

func TestAPIErrorRedactsEchoedBody(t *testing.T) {
	e := &APIError{Method: "GET", Path: "/api/v1/x", StatusCode: 400, Body: `{"m":"/x?token=SEKT1&keep=1"}`}
	if got := e.Error(); strings.Contains(got, "SEKT1") || !strings.Contains(got, "keep=1") {
		t.Errorf("Error() = %q", got)
	}
}
