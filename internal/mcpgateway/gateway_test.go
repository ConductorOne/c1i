package mcpgateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtractSSEResponse(t *testing.T) {
	// A single result event.
	sse := "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}\n\n"
	got := string(extractSSEResponse([]byte(sse)))
	want := `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`
	if got != want {
		t.Errorf("extractSSEResponse = %q, want %q", got, want)
	}

	// Multiple events: a progress notification precedes the result. The
	// response event (the one carrying `result`) must be returned, not a
	// concatenation of every data line.
	multi := "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"p\":50}}\n\n" +
		"event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"tools\":[]}}\n\n"
	got = string(extractSSEResponse([]byte(multi)))
	want = `{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`
	if got != want {
		t.Errorf("multi-event extractSSEResponse = %q, want %q", got, want)
	}

	// An error event is treated as the response too.
	errSSE := "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"error\":{\"code\":-32601,\"message\":\"nope\"}}\n\n"
	got = string(extractSSEResponse([]byte(errSSE)))
	if !strings.Contains(got, `"error"`) {
		t.Errorf("error-event extractSSEResponse = %q, want the error payload", got)
	}
}

func TestDecodeMessage(t *testing.T) {
	// Empty body (e.g. a 202 to a notification) is not an error.
	if msg, err := decodeMessage([]byte("  ")); err != nil || msg.Error != nil {
		t.Errorf("empty body: got err=%v msg.Error=%v, want nil/nil", err, msg.Error)
	}
	// A JSON-RPC error is surfaced.
	msg, err := decodeMessage([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"Method not found"}}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if msg.Error == nil || msg.Error.Code != -32601 {
		t.Errorf("expected error code -32601, got %+v", msg.Error)
	}
	// A result round-trips.
	msg, err = decodeMessage([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if msg.Error != nil || string(msg.Result) != `{"tools":[]}` {
		t.Errorf("result = %s (err %v)", msg.Result, msg.Error)
	}
}

// TestEndToEnd drives a fake gateway through the full handshake and both RPCs:
// initialize (issuing an Mcp-Session-Id), notifications/initialized, a paginated
// tools/list (two pages via nextCursor), and a tools/call answered over SSE.
func TestEndToEnd(t *testing.T) {
	const wantSession = "sess-abc"
	var gotInitialized bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			ID     *int           `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("server got bad JSON: %v", err)
		}

		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", wantSession)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18"}}`)
		case "notifications/initialized":
			gotInitialized = true
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			// Every request after initialize must echo the session header.
			if r.Header.Get("Mcp-Session-Id") != wantSession {
				t.Errorf("tools/list missing session header: %q", r.Header.Get("Mcp-Session-Id"))
			}
			w.Header().Set("Content-Type", "application/json")
			if req.Params["cursor"] == "page2" {
				_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"b"}]}}`)
			} else {
				_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"a"}],"nextCursor":"page2"}}`)
			}
		case "tools/call":
			// Answer over SSE with a progress notification before the result.
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{}}\n\n")
			_, _ = io.WriteString(w, "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}\n\n")
		default:
			t.Errorf("unexpected method %q", req.Method)
		}
	}))
	defer srv.Close()

	ctx := context.Background()
	c := New(srv.URL, "test-token", srv.Client())

	if err := c.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if !gotInitialized {
		t.Error("server never received notifications/initialized")
	}
	if c.sessionID != wantSession {
		t.Errorf("sessionID = %q, want %q", c.sessionID, wantSession)
	}

	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 2 || tools[0].Name != "a" || tools[1].Name != "b" {
		t.Errorf("ListTools = %+v, want [a b] across two pages", tools)
	}

	result, err := c.CallTool(ctx, "a", json.RawMessage(`{"x":1}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !strings.Contains(string(result), `"ok"`) {
		t.Errorf("CallTool result = %s, want the SSE result event", result)
	}
}

// TestHTTPError verifies a non-2xx gateway response surfaces as *HTTPError
// carrying the status code, so callers can classify auth failures.
func TestHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "forbidden")
	}))
	defer srv.Close()

	c := New(srv.URL, "bad-token", srv.Client())
	err := c.Initialize(context.Background())
	if err == nil {
		t.Fatal("expected an error from a 403 gateway")
	}
	he, ok := err.(*HTTPError)
	if !ok {
		t.Fatalf("error type = %T, want *HTTPError", err)
	}
	if he.StatusCode != http.StatusForbidden {
		t.Errorf("StatusCode = %d, want 403", he.StatusCode)
	}
}
