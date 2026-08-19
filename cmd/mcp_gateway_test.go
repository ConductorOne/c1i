package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
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

// TestGatewayJSONRPCErrorExitCodes pins the process exit code for a JSON-RPC-
// level error returned by tools/call, across every code cmd now
// distinguishes for the gateway: -32602 (invalid params) / -32601 (method
// not found) -- the caller named a tool or method that doesn't exist -- map
// to exitUsage (2); code 0 -- the shape observed for an upstream connector
// failure (see internal/mcpgateway's rpcError.Unwrap) -- maps to exitServer
// (6); and any other code (e.g. -32603, internal error) is left unmapped and
// still exits 1 (generic), exactly as before this change.
//
// It drives a real httptest.Server round trip through *mcpgateway.Client and
// wraps the resulting error exactly as mcp_gateway_call.go's RunE does
// ("tools/call failed: %w", classifyGatewayError(err)), so it proves the
// actual wiring -- classifyGatewayError, rpcError.Unwrap, and exitCode
// together -- rather than re-implementing it. It also pins that the rendered
// error message is unchanged by classification: only the exit code changes.
func TestGatewayJSONRPCErrorExitCodes(t *testing.T) {
	cases := []struct {
		name string
		code int
		want int
	}{
		{"invalid params", -32602, exitUsage},
		{"method not found", -32601, exitUsage},
		{"upstream connector failure", 0, exitServer},
		{"internal error (unmapped code)", -32603, exitError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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
					_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":2,"error":{"code":%d,"message":"boom"}}`, tc.code)
				}
			}))
			defer srv.Close()

			gc := mcpgateway.New(srv.URL, "test-token", srv.Client())
			if err := gc.Initialize(context.Background()); err != nil {
				t.Fatalf("code %d: unexpected Initialize failure: %v", tc.code, err)
			}
			_, callErr := gc.CallTool(context.Background(), "my_tool", nil)
			if callErr == nil {
				t.Fatalf("code %d: expected CallTool to fail", tc.code)
			}

			wrapped := fmt.Errorf("tools/call failed: %w", classifyGatewayError(callErr))
			if got := exitCode(wrapped); got != tc.want {
				t.Errorf("code %d: exitCode = %d, want %d (err: %v)", tc.code, got, tc.want, wrapped)
			}

			// Classification must not change what gets printed -- an agent
			// may be matching on the message.
			wantMsg := fmt.Sprintf("tools/call failed: MCP error %d: boom", tc.code)
			if wrapped.Error() != wantMsg {
				t.Errorf("code %d: message = %q, want %q", tc.code, wrapped.Error(), wantMsg)
			}

			// The gateway answered every one of these cases with HTTP 200 --
			// only the JSON-RPC body carries the error -- so none of them
			// must ever produce a *client.APIError in the chain: that would
			// assert a status the wire never sent. This caught a real bug: an
			// earlier version of the code-0 fix reached exit 6 by unwrapping
			// to *client.APIError{StatusCode: 502}, which then rendered as a
			// false "status":502 in --error-format json for a request that
			// got a real 200.
			var apiErr *client.APIError
			if errors.As(wrapped, &apiErr) {
				t.Errorf("code %d: error chain contains a *client.APIError (status %d) for a JSON-RPC-level failure that never touched HTTP status -- this fabricates a status", tc.code, apiErr.StatusCode)
			}

			var buf bytes.Buffer
			writeError(&buf, wrapped, "json")
			var jsonOut map[string]any
			if err := json.Unmarshal(buf.Bytes(), &jsonOut); err != nil {
				t.Fatalf("code %d: --error-format json output not valid JSON: %v (%s)", tc.code, err, buf.String())
			}
			if _, ok := jsonOut["status"]; ok {
				t.Errorf("code %d: --error-format json output %s carries a \"status\" field for a failure with no real HTTP status", tc.code, buf.String())
			}
		})
	}
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

	t.Run("isError null -> exit 0, output unchanged", func(t *testing.T) {
		resultBody := `{"jsonrpc":"2.0","id":2,"result":{"isError":null,"content":[{"type":"text","text":"ok"}]}}`
		stdout, err := runCall(t, resultBody)
		if err != nil {
			t.Fatalf("expected no error when isError is null (treated as absent), got %v", err)
		}
		if got := exitCode(err); got != exitOK {
			t.Errorf("exitCode = %d, want exitOK", got)
		}
		if !strings.Contains(stdout, "ok") {
			t.Errorf("stdout = %q, want the full result printed unchanged", stdout)
		}
	})

	t.Run("non-object result -> exit 0, output unchanged", func(t *testing.T) {
		// A result that isn't a JSON object at all (no isError key could even
		// exist) must not fail closed.
		resultBody := `{"jsonrpc":"2.0","id":2,"result":[]}`
		stdout, err := runCall(t, resultBody)
		if err != nil {
			t.Fatalf("expected no error for a non-object result, got %v", err)
		}
		if got := exitCode(err); got != exitOK {
			t.Errorf("exitCode = %d, want exitOK", got)
		}
		if strings.TrimSpace(stdout) != "[]" {
			t.Errorf("stdout = %q, want the raw result %q printed unchanged", stdout, "[]")
		}
	})

	// Regression coverage for the "toolResultIsError fails open on a
	// non-boolean isError" bug: a server sending isError as a JSON value
	// other than a literal boolean (string, number, object, array) used to
	// make json.Unmarshal fail with a type-mismatch error, which the old
	// implementation swallowed and reported as false (success) — silently
	// treating a genuine tool failure as exit 0. Each of these must now map
	// to exit 7, with the full result still printed to stdout first.
	nonBooleanIsError := []struct {
		name       string
		resultBody string
	}{
		{"isError string \"true\"", `{"jsonrpc":"2.0","id":2,"result":{"isError":"true","content":[{"type":"text","text":"boom"}]}}`},
		{"isError number 1", `{"jsonrpc":"2.0","id":2,"result":{"isError":1,"content":[{"type":"text","text":"boom"}]}}`},
		{"isError object", `{"jsonrpc":"2.0","id":2,"result":{"isError":{},"content":[{"type":"text","text":"boom"}]}}`},
		{"isError array", `{"jsonrpc":"2.0","id":2,"result":{"isError":[],"content":[{"type":"text","text":"boom"}]}}`},
	}
	for _, tc := range nonBooleanIsError {
		t.Run(tc.name+" -> exit 7 (non-conformant server treated as error, not fail-open)", func(t *testing.T) {
			stdout, err := runCall(t, tc.resultBody)
			if err == nil {
				t.Fatalf("expected an error for non-boolean isError (%s)", tc.name)
			}
			if got := exitCode(err); got != exitToolError {
				t.Errorf("exitCode = %d, want exitToolError(%d); err=%v", got, exitToolError, err)
			}
			var toolErr *toolExecutionError
			if !errors.As(err, &toolErr) {
				t.Errorf("error type = %T, want *toolExecutionError", err)
			}
			// The diagnostic must distinguish this from isError:true so a
			// user can tell a real tool failure from a malformed server
			// response, even though the exit code (7) is the same.
			if strings.Contains(err.Error(), "isError: true") {
				t.Errorf("error message = %q, want it to NOT read like a literal isError:true failure (must distinguish malformed from true)", err.Error())
			}
			if !strings.Contains(err.Error(), "non-boolean") {
				t.Errorf("error message = %q, want it to call out the non-boolean isError value", err.Error())
			}
			// The full result must still be printed first, unaffected by the
			// exit-code classification — the print-then-classify ordering
			// contract holds for this case too, not just isError:true.
			if !strings.Contains(stdout, "boom") {
				t.Errorf("stdout = %q, want the full result (content text) printed before the error is returned", stdout)
			}
		})
	}

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
