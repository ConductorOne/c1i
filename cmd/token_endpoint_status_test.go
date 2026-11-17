package cmd

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-jose/go-jose/v4"
)

// tokenEndpointSecret is a client secret in the "v1" wire format around a
// fresh ed25519 key, the only shape the token source accepts.
func tokenEndpointSecret(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := (&jose.JSONWebKey{Key: priv, KeyID: "k1", Algorithm: "EdDSA", Use: "sig"}).MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return "id:ignored:v1:" + base64.RawURLEncoding.EncodeToString(raw)
}

// runWithTokenEndpoint runs the real CLI against a TLS token endpoint that
// answers every request with status, and returns the exit code, the number of
// token requests, and everything the command printed. Retry-After: 0 keeps
// each backoff at the transport's 100ms floor.
func runWithTokenEndpoint(t *testing.T, status int, args ...string) (exit int, hits int32, printed string) {
	t.Helper()
	exit, hits, printed, _ = runTokenEndpointErr(t, status, args...)
	return exit, hits, printed
}

// runTokenEndpointErr is runWithTokenEndpoint that also returns the error.
// Status 200 answers with a non-JSON body; 0 drops the connection.
func runTokenEndpointErr(t *testing.T, status int, args ...string) (exit int, hits int32, printed string, err error) {
	t.Helper()
	t.Setenv("C1I_NO_TOKEN_CACHE", "1")
	return runAgainst(t, &hits, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/v1/token" {
			t.Errorf("unexpected request to %s", r.URL.Path)
		}
		atomic.AddInt32(&hits, 1)
		if status == 0 { // simulate a transport failure: drop the connection
			conn, _, _ := w.(http.Hijacker).Hijack()
			_ = conn.Close()
			return
		}
		if status == 200 {
			_, _ = w.Write([]byte("<html>not json</html>"))
			return
		}
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(status)
	}, args...)
}

// runAgainst runs the real CLI against a TLS server answering with h, trusting
// its certificate, and returns what runTokenEndpointErr documents.
func runAgainst(t *testing.T, hits *int32, h http.HandlerFunc, args ...string) (exit int, _ int32, printed string, err error) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	dt := http.DefaultTransport.(*http.Transport)
	orig := dt.TLSClientConfig
	dt.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	t.Cleanup(func() { dt.TLSClientConfig = orig })

	secret := tokenEndpointSecret(t)
	t.Setenv("C1I_URL", srv.URL)
	t.Setenv("C1I_CLIENT_ID", "id")
	t.Setenv("C1I_CLIENT_SECRET", secret)

	resetCmds(t, rootCmd, authTokenCmd, apiCmd)
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs(args)
	// Cobra keeps a subcommand's first context, so pin this test's.
	authTokenCmd.SetContext(t.Context())
	apiCmd.SetContext(t.Context())
	t.Cleanup(func() { // don't leak this test's cancelled context to later tests
		authTokenCmd.SetContext(context.Background())
		apiCmd.SetContext(context.Background())
	})
	stderr := captureStderr(t, func() { err = executeRoot() })
	if err == nil {
		t.Fatalf("%v: want an error, got none", args)
	}
	printed = out.String() + stderr + err.Error()
	for _, secretish := range []string{secret, "client_assertion", "eyJ"} {
		if strings.Contains(printed, secretish) {
			t.Errorf("%v: output leaks %q:\n%s", args, secretish, printed)
		}
	}
	return exitCode(err), atomic.LoadInt32(hits), printed, err
}

// TestTokenEndpointStatusClassification: a bad-credential status is an auth
// failure and is never retried; 429 and 5xx come from the token service, so
// they are retried like any other 429/5xx and exit 5/6 when they persist.
func TestTokenEndpointStatusClassification(t *testing.T) {
	const retries = 2
	cases := []struct {
		status   int
		wantExit int
		wantHits int32
	}{
		{400, exitAuth, 1},
		{401, exitAuth, 1},
		{403, exitAuth, 1},
		{429, exitRateLimited, retries + 1},
		{500, exitServer, retries + 1},
		{503, exitServer, retries + 1},
		{404, exitNotFound, 1}, // not a credential rejection; classified like any API 404
		{200, exitServer, 1},   // a 200 whose body is not a token
	}
	cmds := map[string][]string{
		"auth token": {"auth", "token", "--max-retries", fmt.Sprint(retries)},
		"api":        {"api", "--path", "/api/v1/users", "--method", "GET", "--max-retries", fmt.Sprint(retries)},
	}
	for name, args := range cmds {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("%s/%d", name, tc.status), func(t *testing.T) {
				exit, hits, printed := runWithTokenEndpoint(t, tc.status, args...)
				if exit != tc.wantExit {
					t.Errorf("exit = %d, want %d (output: %s)", exit, tc.wantExit, printed)
				}
				if hits != tc.wantHits {
					t.Errorf("token requests = %d, want %d", hits, tc.wantHits)
				}
			})
		}
	}
}

func TestTokenEndpointMaxRetriesZeroSendsOnce(t *testing.T) {
	for _, status := range []int{429, 503} {
		exit, hits, _ := runWithTokenEndpoint(t, status, "auth", "token", "--max-retries", "0")
		if hits != 1 {
			t.Errorf("status %d: token requests = %d, want 1", status, hits)
		}
		if want := map[int]int{429: exitRateLimited, 503: exitServer}[status]; exit != want {
			t.Errorf("status %d: exit = %d, want %d", status, exit, want)
		}
	}
}

// A transport failure reaching the token endpoint is not a credential
// rejection: it is retried once per budget (not once per layer) and is
// classified like any other transport failure, not as exit 3.
func TestTokenEndpointTransportFailureIsNotAuth(t *testing.T) {
	for name, args := range map[string][]string{
		"auth token": {"auth", "token", "--max-retries", "1"},
		"api":        {"api", "--path", "/api/v1/users", "--method", "GET", "--max-retries", "1"},
	} {
		t.Run(name, func(t *testing.T) {
			exit, hits, printed := runWithTokenEndpoint(t, 0, args...)
			if exit == exitAuth {
				t.Errorf("exit = %d, a transport failure must not read as rejected credentials (output: %s)", exit, printed)
			}
			if hits != 2 {
				t.Errorf("token requests = %d, want 2 (retried once per budget)", hits)
			}
		})
	}
}

// A token-request failure happened before the mutation was sent, so the error
// must say applied:false even when the mint was retried.
func TestTokenEndpointFailureOnMutationIsNotApplied(t *testing.T) {
	resetCmds(t, appsCreateCmd)
	_, _, _, err := runTokenEndpointErr(t, 503, "apps", "create", "--display-name", "X", "--max-retries", "2")
	var buf bytes.Buffer
	writeError(&buf, err, "json", appsCreateCmd)
	var obj map[string]any
	if jerr := json.Unmarshal(buf.Bytes(), &obj); jerr != nil {
		t.Fatalf("not JSON: %q", buf.String())
	}
	if obj["applied"] != false {
		t.Errorf("applied = %v, want false: %s", obj["applied"], buf.String())
	}
}

// The token expires inside expirySkew, so every request re-mints. The DELETE
// reaches the server (503) and is retried; the re-mint before the retry then
// fails. The mutation may have applied, so the answer is unknown, not false.
func TestMintFailureAfterSentMutationIsUnknown(t *testing.T) {
	var tokenHits, deleteHits int32
	resetCmds(t, appsDeleteCmd)
	_, _, _, err := runAgainst(t, &tokenHits, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0")
		switch {
		case r.URL.Path == "/auth/v1/token" && atomic.AddInt32(&tokenHits, 1) == 1:
			_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"Bearer","expires_in":30}`))
		case r.URL.Path == "/auth/v1/token":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			atomic.AddInt32(&deleteHits, 1)
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}, "apps", "delete", "a1", "--max-retries", "1")
	if deleteHits != 1 {
		t.Fatalf("DELETE requests = %d, want 1 (the scenario needs a received DELETE)", deleteHits)
	}
	var buf bytes.Buffer
	writeError(&buf, err, "json", appsDeleteCmd)
	var obj map[string]any
	if jerr := json.Unmarshal(buf.Bytes(), &obj); jerr != nil {
		t.Fatalf("not JSON: %q", buf.String())
	}
	if obj["applied"] != "unknown" {
		t.Errorf("applied = %v, want \"unknown\": %s", obj["applied"], buf.String())
	}
}
