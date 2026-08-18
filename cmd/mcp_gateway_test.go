package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ConductorOne/c1i/internal/mcpgateway"
)

func TestDeriveGatewayURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://leet.conductor.one", "https://leet-mcp.conductor.one/v1"},
		{"https://acme.conductor.one/", "https://acme-mcp.conductor.one/v1"},
		{"http://localhost:8080", "http://localhost-mcp:8080/v1"},
	}
	for _, c := range cases {
		got, err := deriveGatewayURL(c.in)
		if err != nil {
			t.Errorf("deriveGatewayURL(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("deriveGatewayURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if _, err := deriveGatewayURL("::not a url"); err == nil {
		t.Error("expected error for unparseable base URL")
	}
}

// TestGatewayErrorExitCodes pins the process exit code for a gateway HTTP
// failure at every status the CLI's taxonomy distinguishes (cmd/errors.go's
// exitCode: 401/403 auth, 404 not-found, 429 rate-limited, 5xx server), for
// both failure points a gateway command can hit:
//
//   - the handshake (Initialize), wrapped exactly as newGatewayClient does
//     ("gateway handshake failed: %w")
//   - a call made after a successful handshake (tools/list), wrapped exactly
//     as mcp_gateway_list_tools.go's RunE does ("tools/list failed: %w") —
//     this is the path PR #49 left unclassified entirely, since
//     classifyGatewayErr's only call site was the handshake.
//
// It drives a real httptest.Server round trip through *mcpgateway.Client
// (the same client newGatewayClient builds) rather than constructing an
// error value by hand, so it proves the actual wiring: HTTPError unwraps to
// a *client.APIError, and that error type is what exitCode classifies on.
func TestGatewayErrorExitCodes(t *testing.T) {
	statusToExit := []struct {
		status int
		want   int
	}{
		{http.StatusUnauthorized, exitAuth},
		{http.StatusForbidden, exitAuth},
		{http.StatusNotFound, exitNotFound},
		{http.StatusTooManyRequests, exitRateLimited},
		{http.StatusInternalServerError, exitServer},
		{http.StatusServiceUnavailable, exitServer},
	}

	t.Run("handshake", func(t *testing.T) {
		for _, tc := range statusToExit {
			t.Run(fmt.Sprintf("%d", tc.status), func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte("boom"))
				}))
				defer srv.Close()

				gc := mcpgateway.New(srv.URL, "test-token", srv.Client())
				err := gc.Initialize(context.Background())
				if err == nil {
					t.Fatalf("status %d: expected Initialize to fail", tc.status)
				}
				wrapped := fmt.Errorf("gateway handshake failed: %w", err)
				if got := exitCode(wrapped); got != tc.want {
					t.Errorf("status %d: exitCode = %d, want %d (err: %v)", tc.status, got, tc.want, wrapped)
				}
			})
		}
	})

	t.Run("post-handshake tools/list", func(t *testing.T) {
		for _, tc := range statusToExit {
			t.Run(fmt.Sprintf("%d", tc.status), func(t *testing.T) {
				reqN := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					reqN++
					switch reqN {
					case 1: // initialize succeeds
						w.Header().Set("Content-Type", "application/json")
						_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
					case 2: // notifications/initialized
						w.WriteHeader(http.StatusAccepted)
					default: // tools/list fails
						w.WriteHeader(tc.status)
						_, _ = w.Write([]byte("boom"))
					}
				}))
				defer srv.Close()

				gc := mcpgateway.New(srv.URL, "test-token", srv.Client())
				if err := gc.Initialize(context.Background()); err != nil {
					t.Fatalf("status %d: unexpected Initialize failure: %v", tc.status, err)
				}
				_, err := gc.ListTools(context.Background())
				if err == nil {
					t.Fatalf("status %d: expected ListTools to fail", tc.status)
				}
				wrapped := fmt.Errorf("tools/list failed: %w", err)
				if got := exitCode(wrapped); got != tc.want {
					t.Errorf("status %d: exitCode = %d, want %d (err: %v)", tc.status, got, tc.want, wrapped)
				}
			})
		}
	})
}
