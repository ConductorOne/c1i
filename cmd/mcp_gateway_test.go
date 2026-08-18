package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/mcpgateway"
	"github.com/spf13/cobra"
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

// TestGatewayCallIsErrorExitCode covers the four required scenarios for a
// tool's own reported failure: a tools/call result with isError:true (exit 7,
// full result still printed), a result with isError:false or with isError
// absent (both exit 0, output unchanged), and a JSON-RPC error response
// (must NOT map to exit 7 — that path is a transport/protocol failure with
// its own existing classification, unrelated to a tool reporting its own
// failure).
//
// Each case drives a real httptest.Server round trip through *mcpgateway.Client
// (as newGatewayClient would build, minus the auth step, which is orthogonal
// to what's under test here) and then calls renderCallResult — the exact
// function mcpGatewayCallCmd's RunE calls — so this exercises production
// code, not a re-implementation of it.
func TestGatewayCallIsErrorExitCode(t *testing.T) {
	// runCall drives Initialize + CallTool against a server that answers
	// tools/call with resultBody (a raw JSON-RPC result/error string), then
	// runs renderCallResult exactly as the real command does. Returns the
	// resulting error and everything written to the command's stdout.
	runCall := func(t *testing.T, resultBody string) (stdout string, err error) {
		reqN := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqN++
			w.Header().Set("Content-Type", "application/json")
			switch reqN {
			case 1: // initialize
				_, _ = fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
			case 2: // notifications/initialized
				w.WriteHeader(http.StatusAccepted)
			default: // tools/call
				_, _ = fmt.Fprint(w, resultBody)
			}
		}))
		defer srv.Close()

		gc := mcpgateway.New(srv.URL, "test-token", srv.Client())
		if ierr := gc.Initialize(context.Background()); ierr != nil {
			t.Fatalf("Initialize: %v", ierr)
		}
		result, callErr := gc.CallTool(context.Background(), "my_tool", nil)

		cmd := &cobra.Command{}
		var buf bytes.Buffer
		cmd.SetOut(&buf)

		if callErr != nil {
			return buf.String(), fmt.Errorf("tools/call failed: %w", callErr)
		}
		renderErr := renderCallResult(cmd, "my_tool", result)
		return buf.String(), renderErr
	}

	t.Run("isError true -> exit 7, full result still printed", func(t *testing.T) {
		resultBody := `{"jsonrpc":"2.0","id":2,"result":{"isError":true,"content":[{"type":"text","text":"boom"}]}}`
		stdout, err := runCall(t, resultBody)
		if err == nil {
			t.Fatal("expected an error for isError:true")
		}
		if got := exitCode(err); got != exitToolError {
			t.Errorf("exitCode = %d, want exitToolError(%d); err=%v", got, exitToolError, err)
		}
		var toolErr *toolExecutionError
		if !errors.As(err, &toolErr) {
			t.Errorf("error type = %T, want *toolExecutionError", err)
		}
		// The full result (isError and all) must still be on stdout,
		// unaffected by the exit-code classification.
		if !strings.Contains(stdout, `"isError": true`) || !strings.Contains(stdout, "boom") {
			t.Errorf("stdout = %q, want the full result (isError:true, content text) printed", stdout)
		}
	})

	t.Run("isError false -> exit 0, output unchanged", func(t *testing.T) {
		resultBody := `{"jsonrpc":"2.0","id":2,"result":{"isError":false,"content":[{"type":"text","text":"ok"}]}}`
		stdout, err := runCall(t, resultBody)
		if err != nil {
			t.Fatalf("expected no error for isError:false, got %v", err)
		}
		if got := exitCode(err); got != exitOK {
			t.Errorf("exitCode = %d, want exitOK", got)
		}
		if !strings.Contains(stdout, `"isError": false`) || !strings.Contains(stdout, "ok") {
			t.Errorf("stdout = %q, want the full result printed unchanged", stdout)
		}
	})

	t.Run("isError absent -> exit 0, output unchanged", func(t *testing.T) {
		resultBody := `{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"ok"}]}}`
		stdout, err := runCall(t, resultBody)
		if err != nil {
			t.Fatalf("expected no error when isError is absent, got %v", err)
		}
		if got := exitCode(err); got != exitOK {
			t.Errorf("exitCode = %d, want exitOK", got)
		}
		if !strings.Contains(stdout, "ok") {
			t.Errorf("stdout = %q, want the full result printed unchanged", stdout)
		}
	})

	t.Run("JSON-RPC error response does not map to exit 7", func(t *testing.T) {
		resultBody := `{"jsonrpc":"2.0","id":2,"error":{"code":-32602,"message":"unknown tool"}}`
		_, err := runCall(t, resultBody)
		if err == nil {
			t.Fatal("expected an error for a JSON-RPC error response")
		}
		if got := exitCode(err); got == exitToolError {
			t.Errorf("exitCode = %d, must NOT be exitToolError(%d) for a transport/protocol-level JSON-RPC error", got, exitToolError)
		}
		var toolErr *toolExecutionError
		if errors.As(err, &toolErr) {
			t.Error("a JSON-RPC error response must not be classified as *toolExecutionError")
		}
	})
}
