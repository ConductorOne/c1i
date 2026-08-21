package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/tokensource"
	"github.com/zalando/go-keyring"
)

// newTestClient builds a Client that sends every request through the shared
// transport (retry/backoff, redirect guard, path guard, user-agent) to srv,
// bypassing loadCredentials and the OAuth mint New performs — the same
// construction NewForTesting exposes for cross-package use.
func newTestClient(srv *httptest.Server, maxRetries int) *Client {
	return NewForTesting(srv.URL, srv.Client(), WithMaxRetries(maxRetries))
}

func TestPath(t *testing.T) {
	tests := []struct {
		name   string
		format string
		ids    []string
		want   string
	}{
		{
			name:   "single segment",
			format: "/api/v1/functions/%s",
			ids:    []string{"abc123"},
			want:   "/api/v1/functions/abc123",
		},
		{
			name:   "multiple segments",
			format: "/api/v1/apps/%s/connectors/%s/mcp_tools/%s",
			ids:    []string{"app1", "conn2", "tool3"},
			want:   "/api/v1/apps/app1/connectors/conn2/mcp_tools/tool3",
		},
		{
			name:   "escapes reserved characters",
			format: "/api/v1/tasks/%s/action/deny",
			ids:    []string{"a?b#c d"},
			want:   "/api/v1/tasks/a%3Fb%23c%20d/action/deny",
		},
		{
			name:   "escapes slashes so a value cannot traverse segments",
			format: "/api/v1/functions/%s",
			ids:    []string{"a/b"},
			want:   "/api/v1/functions/a%2Fb",
		},
		{
			name:   "no ids returns format unchanged",
			format: "/api/v1/apps",
			ids:    nil,
			want:   "/api/v1/apps",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Path(tt.format, tt.ids...); got != tt.want {
				t.Errorf("Path(%q, %v) = %q, want %q", tt.format, tt.ids, got, tt.want)
			}
		})
	}
}

func TestPathLiteralPercent(t *testing.T) {
	// %% is a literal percent, not a verb, so it needs no id.
	if got := Path("/api/v1/x/%s/y%%z", "id"); got != "/api/v1/x/id/y%z" {
		t.Errorf("Path with literal %%%% = %q", got)
	}
}

func TestPathPanicsOnMismatch(t *testing.T) {
	cases := []struct {
		name   string
		format string
		ids    []string
	}{
		{"too few ids", "/api/v1/apps/%s/connectors/%s", []string{"a"}},
		{"too many ids", "/api/v1/apps/%s", []string{"a", "b"}},
		{"unsupported verb", "/api/v1/apps/%d", []string{"a"}},
		{"dangling percent", "/api/v1/apps/%", []string{"a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("Path(%q, %v) did not panic", tc.format, tc.ids)
				}
			}()
			_ = Path(tc.format, tc.ids...)
		})
	}
}

type errRoundTripper struct{ err error }

func (r errRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, r.err
}

func TestDoWrapsTokenErrorAsAuth(t *testing.T) {
	// A rejected client_credentials grant surfaces from the transport's
	// Do (wrapped in *url.Error). do() should classify it as an AuthError.
	c := NewForTesting("https://example.conductor.one", &http.Client{Transport: errRoundTripper{&tokensource.TokenError{StatusCode: 401}}}, WithMaxRetries(0))
	_, err := c.Get(context.Background(), "/api/v1/x", nil)
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected AuthError for token rejection, got %T: %v", err, err)
	}
}

func TestDoNetworkErrorIsNotAuth(t *testing.T) {
	c := NewForTesting("https://example.conductor.one", &http.Client{Transport: errRoundTripper{errors.New("dial tcp: connection refused")}}, WithMaxRetries(0))
	_, err := c.Get(context.Background(), "/api/v1/x", nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	var authErr *AuthError
	if errors.As(err, &authErr) {
		t.Error("a plain network error must not be classified as AuthError")
	}
}

// TestLoadCredentialsKeyringUnavailableStillClassifiesAsAuthError exercises
// loadCredentials end-to-end (real keychain package, mocked OS keyring) for
// the diagnosis case: the keyring is unreachable and the file store has no
// entry either. The diagnostic wording keychain.Load now returns for that
// case must still surface as *AuthError — cmd/errors.go's exitCode maps
// *AuthError to exit 3 regardless of message text, so a diagnosability fix
// here must not change what error type wraps it.
func TestLoadCredentialsKeyringUnavailableStillClassifiesAsAuthError(t *testing.T) {
	keyring.MockInitWithError(keyring.ErrUnsupportedPlatform)
	dir := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("AppData", dir)
	case "darwin":
		t.Setenv("HOME", dir)
	default:
		t.Setenv("XDG_CONFIG_HOME", dir)
	}
	t.Setenv("C1I_CLIENT_ID", "")
	t.Setenv("C1I_CLIENT_SECRET", "")
	_ = os.Unsetenv("C1I_CLIENT_ID")
	_ = os.Unsetenv("C1I_CLIENT_SECRET")

	// example.test is not a *.conductor.one host, so loadCredentials makes a
	// single keychain.Load call with no legacy-key fallback to complicate it.
	_, _, err := loadCredentials("https://example.test")
	if err == nil {
		t.Fatal("expected an error: keyring unavailable and no file-backed credential")
	}
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *AuthError, got %T: %v", err, err)
	}
	if !strings.Contains(authErr.Error(), "keyring is currently unavailable") {
		t.Errorf("AuthError = %q, want it to carry the keyring-unavailable diagnosis", authErr.Error())
	}
}

func TestDoReturnsAPIErrorWithStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"nope"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	c := newTestClient(srv, 0)
	_, err := c.Get(context.Background(), "/api/v1/x", nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 404 || apiErr.Method != http.MethodGet {
		t.Errorf("APIError = %+v, want status 404 / GET", apiErr)
	}
}

func TestRequestSendsMethodHeaderBody(t *testing.T) {
	var gotMethod, gotHeader, gotCT, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotHeader = r.Header.Get("X-Custom")
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	out, err := newTestClient(srv, 0).Request(context.Background(), http.MethodPatch, "/x",
		[]byte(`{"a":1}`), map[string]string{"X-Custom": "yes"})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", gotMethod)
	}
	if gotHeader != "yes" {
		t.Errorf("X-Custom = %q, want yes", gotHeader)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotCT)
	}
	if gotBody != `{"a":1}` {
		t.Errorf("body = %q, want {\"a\":1}", gotBody)
	}
	if string(out) != `{"ok":true}` {
		t.Errorf("out = %q", out)
	}
}

func TestRequestNoBodyOmitsContentType(t *testing.T) {
	var gotCT string
	var hadBody bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		hadBody = len(b) > 0
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	if _, err := newTestClient(srv, 0).Request(context.Background(), http.MethodGet, "/x", nil, nil); err != nil {
		t.Fatalf("Request: %v", err)
	}
	if gotCT != "" {
		t.Errorf("Content-Type = %q, want empty for bodyless request", gotCT)
	}
	if hadBody {
		t.Errorf("expected no request body")
	}
}

func TestPatchSendsBodyAndContentType(t *testing.T) {
	var gotMethod, gotCT, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	if _, err := newTestClient(srv, 0).Patch(context.Background(), "/x", map[string]any{"hello": "world"}); err != nil {
		t.Fatalf("Patch: %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", gotMethod)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotCT)
	}
	if gotBody != `{"hello":"world"}` {
		t.Errorf("body = %q", gotBody)
	}
}
