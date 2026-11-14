package transport

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// A transport error, the debug trace and a redirect refusal never carry a
// secret query value or userinfo, and the error keeps its *url.Error type.
func TestSecretsNeverReachErrorsOrTrace(t *testing.T) {
	const q = "/x?token=QSEKR3T&ok=1"
	t.Run("transport error", func(t *testing.T) {
		var dbg bytes.Buffer
		c := New(http.DefaultTransport, WithMaxRetries(0), WithDebug(&dbg))
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://ghp_USER1SEKR3T@127.0.0.1:1"+q, nil)
		_, err := c.Do(req)
		var ue *url.Error
		if !errors.As(err, &ue) {
			t.Fatalf("err = %v, want a *url.Error in the chain", err)
		}
		for _, s := range []string{err.Error(), dbg.String()} {
			if strings.Contains(s, "QSEKR3T") || strings.Contains(s, "USER1SEKR3T") || !strings.Contains(s, "ok=1") {
				t.Errorf("leaked or lost the visible param: %q", s)
			}
		}
	})
	t.Run("redirect refusal", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", "/other?token=LSEKR3T")
			w.WriteHeader(http.StatusFound)
		}))
		defer srv.Close()
		c := New(http.DefaultTransport, WithMaxRetries(0))
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+q, nil)
		_, err := c.Do(req)
		var re *RedirectError
		if !errors.As(err, &re) {
			t.Fatalf("err = %v, want a *RedirectError", err)
		}
		if s := err.Error(); strings.Contains(s, "QSEKR3T") || strings.Contains(s, "LSEKR3T") {
			t.Errorf("leaked: %q", s)
		}
	})
}
