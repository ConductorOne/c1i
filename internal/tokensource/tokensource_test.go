package tokensource

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ConductorOne/c1i/internal/transport"
	"github.com/go-jose/go-jose/v4"
)

// validSecret builds a client secret in this package's "v1" wire format
// (clientID:???:v1:base64(jwk)) around a freshly generated ed25519 key, the
// only shape parseSecret accepts.
func validSecret(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	jwk := jose.JSONWebKey{Key: priv, KeyID: "k1", Algorithm: "EdDSA", Use: "sig"}
	raw, err := jwk.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return "id:ignored:v1:" + base64.RawURLEncoding.EncodeToString(raw)
}

// newTestTokenSource builds a *c1TokenSource pointed at a TLS test server,
// bypassing NewTokenSource's fixed transport.New(nil, ...) so the request can
// trust the server's test certificate. tokenHost is always minted over https
// (see Token's tokenURL), so an httptest.NewServer (plain HTTP) can't stand
// in for the real token endpoint here the way it can for login/gateway.
func newTestTokenSource(t *testing.T, srv *httptest.Server, opts ...transport.Option) *c1TokenSource {
	t.Helper()
	secret, err := parseSecret([]byte(validSecret(t)))
	if err != nil {
		t.Fatal(err)
	}
	return &c1TokenSource{
		clientID:     "client1",
		clientSecret: secret,
		tokenHost:    strings.TrimPrefix(srv.URL, "https://"),
		transport:    transport.New(srv.Client().Transport, opts...),
	}
}

func TestTokenSource_MintsSuccessfully(t *testing.T) {
	var gotUA string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		if r.URL.Path != "/auth/v1/token" {
			t.Errorf("path = %q, want /auth/v1/token", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"access_token":"abc123","token_type":"Bearer","expires_in":3600}`))
	}))
	defer srv.Close()

	tok, err := newTestTokenSource(t, srv).Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok.AccessToken != "abc123" {
		t.Errorf("AccessToken = %q, want abc123", tok.AccessToken)
	}
	// User-Agent proves the mint goes through the shared transport, not a
	// bare http.Client sending Go's default User-Agent.
	if !strings.HasPrefix(gotUA, "c1.ai/c1i") {
		t.Errorf("User-Agent = %q, want it to start with c1.ai/c1i", gotUA)
	}
}

func TestTokenSource_NonOKIsTokenError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := newTestTokenSource(t, srv).Token()
	if err == nil {
		t.Fatal("expected an error")
	}
	tokErr, ok := err.(*TokenError)
	if !ok {
		t.Fatalf("error = %T (%v), want *TokenError", err, err)
	}
	if tokErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", tokErr.StatusCode)
	}
}

// TestTokenSource_TimesOut proves a hung token host doesn't block a mint
// forever: before this package used internal/transport, the mint's
// http.Client had a fixed 30s Timeout, which this test would have had to
// wait out to observe. Here the timeout is passed explicitly via
// transport.WithTimeout so the test doesn't depend on (and can't be
// defeated by a future change to) NewTokenSource's own default.
func TestTokenSource_TimesOut(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	defer srv.Close()
	defer close(block) // registered after srv.Close(): runs first (LIFO), so Close doesn't wait on the still-active connection

	ts := newTestTokenSource(t, srv, transport.WithTimeout(50*time.Millisecond))

	done := make(chan error, 1)
	go func() {
		_, err := ts.Token()
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a timeout error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Token did not return within 5s -- a hung token host must time out, not hang")
	}
}

// TestNewTokenSource_Constructs proves the public constructor itself parses
// the secret and builds a usable TokenSource (the wiring c1TokenSource's own
// tests above bypass via newTestTokenSource, since NewTokenSource always
// dials https over http.DefaultTransport and so can't trust a local test
// server's self-signed certificate). tokenRequestTimeout always overriding a
// caller-supplied transport.WithTimeout (see NewTokenSource's doc comment
// and its append-last ordering) is verified by inspection, not exercised
// here, for the same reason.
func TestNewTokenSource_Constructs(t *testing.T) {
	ts, err := NewTokenSource(context.Background(), "client1", validSecret(t), "example.test")
	if err != nil {
		t.Fatalf("NewTokenSource: %v", err)
	}
	if ts == nil {
		t.Fatal("expected a non-nil TokenSource")
	}
}

func TestNewTokenSource_RejectsMalformedSecret(t *testing.T) {
	if _, err := NewTokenSource(context.Background(), "client1", "not-a-valid-secret", "example.test"); err != ErrInvalidClientSecret {
		t.Errorf("NewTokenSource error = %v, want ErrInvalidClientSecret", err)
	}
}

func TestParseSecretRejectsMalformed(t *testing.T) {
	cases := []string{
		"",
		"only:three:parts",
		"id:ignored:v2:whatever", // wrong version tag
		"id:ignored:v1:not-valid-base64!!",
	}
	for _, c := range cases {
		if _, err := parseSecret([]byte(c)); err != ErrInvalidClientSecret {
			t.Errorf("parseSecret(%q) = %v, want ErrInvalidClientSecret", c, err)
		}
	}
}

func TestTokenSource_EmptyAccessTokenIsError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"","token_type":"Bearer","expires_in":3600}`))
	}))
	defer srv.Close()

	if _, err := newTestTokenSource(t, srv).Token(); err == nil {
		t.Fatal("expected an error for an empty access token")
	}
}

func TestTokenSource_NonCredentialStatusIsMintError(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable} {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
		_, err := newTestTokenSource(t, srv, transport.WithMaxRetries(0)).Token()
		srv.Close()
		var mintErr *MintError
		if !errors.As(err, &mintErr) {
			t.Fatalf("status %d: error = %T (%v), want *MintError", status, err, err)
		}
		var apiErr *transport.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != status {
			t.Errorf("status %d: error = %v, want a *transport.APIError carrying the status", status, err)
		}
		var tokErr *TokenError
		if errors.As(err, &tokErr) {
			t.Errorf("status %d: must not be a *TokenError", status)
		}
	}
}

// Each attempt must sign a fresh assertion: a replayed one expires (2m) during
// a long Retry-After wait and the final attempt would 401.
func TestTokenSource_RetryResignsAssertion(t *testing.T) {
	base := time.Unix(1_800_000_000, 0)
	var calls int
	origNow := now
	now = func() time.Time { calls++; return base.Add(time.Duration(calls-1) * 3 * time.Minute) }
	t.Cleanup(func() { now = origNow })

	var iats []int64
	var nonces, lens []string
	var contentLens []int64
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		a := r.PostForm.Get("client_assertion")
		parts := strings.Split(a, ".")
		raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
		var c struct{ Iat, Exp int64 }
		_ = json.Unmarshal(raw, &c)
		hdr, _ := base64.RawURLEncoding.DecodeString(parts[0])
		var h struct{ Nonce string }
		_ = json.Unmarshal(hdr, &h)
		iats = append(iats, c.Iat)
		nonces = append(nonces, h.Nonce)
		lens = append(lens, fmt.Sprint(len(a)))
		contentLens = append(contentLens, r.ContentLength)
		if c.Exp <= c.Iat {
			t.Errorf("exp %d not after iat %d", c.Exp, c.Iat)
		}
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, _ = newTestTokenSource(t, srv, transport.WithMaxRetries(2), transport.WithReadOnly()).Token()
	if len(iats) != 3 {
		t.Fatalf("attempts = %d, want 3", len(iats))
	}
	for i, iat := range iats {
		if want := base.Add(time.Duration(i) * 3 * time.Minute).Unix(); iat != want {
			t.Errorf("attempt %d iat = %d, want %d (a fresh assertion signed at attempt time)", i, iat, want)
		}
		if i > 0 && (nonces[i] == nonces[i-1] || lens[i] != lens[0] || contentLens[i] != contentLens[0]) {
			t.Errorf("attempt %d: nonce/length not fresh-and-constant: %v %v %v", i, nonces, lens, contentLens)
		}
	}
}

func TestTokenSource_UnusableTokenResponseIsMintError(t *testing.T) {
	for _, body := range []string{`<html>`, `{"access_token":""}`} {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		_, err := newTestTokenSource(t, srv).Token()
		srv.Close()
		var mintErr *MintError
		if !errors.As(err, &mintErr) || !errors.Is(err, ErrBadTokenResponse) {
			t.Errorf("body %q: error = %v, want a *MintError wrapping ErrBadTokenResponse", body, err)
		}
	}
}

func TestRejectsCredentialsIsExactlyBadCredentialStatuses(t *testing.T) {
	for status := 100; status < 600; status++ {
		want := status == 400 || status == 401 || status == 403
		if got := rejectsCredentials(status); got != want {
			t.Errorf("rejectsCredentials(%d) = %v, want %v", status, got, want)
		}
	}
}

// A re-signed body of a different length must fail the retry, not be sent
// under the original ContentLength.
func TestTokenSource_RetryFailsClosedOnBodyLengthChange(t *testing.T) {
	var n int
	orig := newNonce
	newNonce = func() (string, error) {
		n++
		return strings.Repeat("a", 31+n), nil
	}
	t.Cleanup(func() { newNonce = orig })

	var hits int
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := newTestTokenSource(t, srv, transport.WithMaxRetries(1), transport.WithReadOnly()).Token()
	if err == nil || !strings.Contains(err.Error(), "re-signed token body changed length") {
		t.Fatalf("error = %v, want the body-length refusal", err)
	}
	if hits != 1 {
		t.Errorf("requests = %d, want 1 (the retry must not be sent)", hits)
	}
}
