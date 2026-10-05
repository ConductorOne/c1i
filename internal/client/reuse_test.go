package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

type countingSource struct{ mints atomic.Int32 }

func (s *countingSource) Token() (*oauth2.Token, error) {
	s.mints.Add(1)
	return &oauth2.Token{AccessToken: "tok", Expiry: time.Now().Add(time.Hour)}, nil
}

// TestNewWithCredentialsReusesToken pins that the never-stored client mints
// once, not per request: each mint is an audited client_credentials exchange.
func TestNewWithCredentialsReusesToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	src := &countingSource{}
	c := newReusing(srv.URL, src, []Option{WithMaxRetries(0)})
	for i := 0; i < 3; i++ {
		if _, err := c.Get(context.Background(), "/api/v1/auth/introspect", nil); err != nil {
			t.Fatalf("Get %d: %v", i, err)
		}
	}
	if n := src.mints.Load(); n != 1 {
		t.Errorf("minted %d tokens for 3 requests, want 1", n)
	}
}
