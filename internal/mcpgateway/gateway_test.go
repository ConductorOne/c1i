package mcpgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
)

func TestExtractSSEResponse(t *testing.T) {
	// A single result event.
	sse := "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}\n\n"
	got := string(extractSSEResponse([]byte(sse), nil))
	want := `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`
	if got != want {
		t.Errorf("extractSSEResponse = %q, want %q", got, want)
	}

	// Multiple events: a progress notification precedes the result. The
	// response event (the one carrying `result`) must be returned, not a
	// concatenation of every data line.
	multi := "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"p\":50}}\n\n" +
		"event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"tools\":[]}}\n\n"
	got = string(extractSSEResponse([]byte(multi), nil))
	want = `{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`
	if got != want {
		t.Errorf("multi-event extractSSEResponse = %q, want %q", got, want)
	}

	// An error event is treated as the response too.
	errSSE := "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"error\":{\"code\":-32601,\"message\":\"nope\"}}\n\n"
	got = string(extractSSEResponse([]byte(errSSE), nil))
	if !strings.Contains(got, `"error"`) {
		t.Errorf("error-event extractSSEResponse = %q, want the error payload", got)
	}
}

// TestExtractSSEResponseMultiDataLineJoin verifies that multiple `data:`
// lines within a single SSE event are joined with "\n" per the SSE spec
// (https://html.spec.whatwg.org/multipage/server-sent-events.html#dispatchMessage),
// not concatenated bare. The event here spans two `data:` lines split right
// before a member separator inside "result", a spec-legal place for an SSE
// producer to wrap a line: the reconstructed bytes must contain the "\n" the
// spec calls for. This is a spec-hardening test (full input space the wire
// format permits — see CLAUDE.md "Adding a new client/subsystem package"),
// not a shape observed from the C1 gateway itself, which has been observed
// answering with plain `application/json` (no SSE framing at all) even for
// long-running calls; the spec still requires this client, which advertises
// SSE support via Accept, to parse it correctly if a server ever sends it.
func TestExtractSSEResponseMultiDataLineJoin(t *testing.T) {
	sse := "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"a\":1\n" +
		"data: ,\"b\":2}}\n" +
		"\n"
	got := string(extractSSEResponse([]byte(sse), nil))
	want := `{"jsonrpc":"2.0","id":1,"result":{"a":1` + "\n" + `,"b":2}}`
	if got != want {
		t.Errorf("extractSSEResponse = %q, want %q (data: lines must join with \\n)", got, want)
	}
	// Bare concatenation (the pre-fix behavior) would instead produce
	// `{"jsonrpc":"2.0","id":1,"result":{"a":1,"b":2}}` — missing the "\n" —
	// which is a DIFFERENT byte sequence from want, so this test fails
	// against the old code and passes against the fixed join logic.
	var probe map[string]any
	if err := json.Unmarshal([]byte(got), &probe); err != nil {
		t.Fatalf("joined payload did not parse as JSON: %v (%q)", err, got)
	}
}

// TestExtractSSEResponseLeadingSpaceStripping verifies that exactly one
// optional leading space after "data:" is stripped, and nothing else —
// meaningful surrounding whitespace inside the payload (a leading space
// beyond the first, or trailing whitespace) must survive.
func TestExtractSSEResponseLeadingSpaceStripping(t *testing.T) {
	// Two leading spaces after "data:": only the first is the SSE field-value
	// separator: the second is part of the value and must be preserved,
	// landing inside the JSON string as a leading space in "text".
	sse := "data:  {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"text\":\"a\"}}   \n\n"
	got := string(extractSSEResponse([]byte(sse), nil))
	want := ` {"jsonrpc":"2.0","id":1,"result":{"text":"a"}}   `
	if got != want {
		t.Errorf("extractSSEResponse = %q, want %q (want leading/trailing whitespace preserved beyond the single stripped separator space)", got, want)
	}
}

// TestExtractSSEResponseSelectsByRequestID verifies that when an SSE stream
// carries a response for a different request's id alongside the response for
// the caller's own id, the caller's id is what selects the event — not
// whichever happens to carry a result/error, and not stream order.
//
// Each arrangement below places at least one DECOY event — a non-matching id
// carrying its own result/error — before the correct event in stream order.
// That positioning matters: tier 2 (the "first event carrying result/error
// wins" fallback) scans in stream order and returns on its first match, so a
// decoy planted ahead of the correct event is exactly what would fool tier 2
// if id-matching (tier 1) didn't exist or were deleted. Without such a decoy
// ahead of the right answer, a subtest can pass "by accident" even with
// id-matching removed, because tier 2 alone happens to land on the correct
// event anyway — which is precisely what made the original "reversed" and
// "with notification" arrangements non-load-bearing (see verification notes
// in the task history: deleting tier 1 only failed the first arrangement).
func TestExtractSSEResponseSelectsByRequestID(t *testing.T) {
	wantID := 7
	want := `{"jsonrpc":"2.0","id":7,"result":{"tools":["right"]}}`

	t.Run("decoy before correct", func(t *testing.T) {
		sse := "data: {\"jsonrpc\":\"2.0\",\"id\":99,\"result\":{\"tools\":[\"wrong\"]}}\n\n" +
			"data: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{\"tools\":[\"right\"]}}\n\n"
		got := string(extractSSEResponse([]byte(sse), &wantID))
		if got != want {
			t.Errorf("extractSSEResponse = %q, want %q (should select the event matching the request id)", got, want)
		}
	})

	// The reverse order must select the same way — it's the id, not order.
	// A decoy (id 123) is now placed BEFORE the correct event, so tier 2
	// alone — without id-matching — would return the decoy instead of
	// stumbling onto the right answer just because it came first in the
	// original two-event arrangement. The original two events are kept
	// as-is, in their original order, with the decoy prepended.
	t.Run("reversed, decoy prepended so tier 2 alone would pick it", func(t *testing.T) {
		sseReversed := "data: {\"jsonrpc\":\"2.0\",\"id\":123,\"result\":{\"tools\":[\"decoy\"]}}\n\n" +
			"data: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{\"tools\":[\"right\"]}}\n\n" +
			"data: {\"jsonrpc\":\"2.0\",\"id\":99,\"result\":{\"tools\":[\"wrong\"]}}\n\n"
		got := string(extractSSEResponse([]byte(sseReversed), &wantID))
		if got != want {
			t.Errorf("extractSSEResponse (reversed) = %q, want %q", got, want)
		}
	})

	// A notification (no id) alongside the wanted response must never be
	// mistaken for the response, even without id matching. A decoy (id 55)
	// is inserted between the notification and the correct event so tier 2
	// alone — which skips the notification anyway, since it carries no
	// result/error — would land on the decoy's result, not the notification
	// being harmlessly skipped past. The original notification-then-correct
	// arrangement is preserved; the decoy is inserted between them.
	t.Run("with notification, decoy inserted so tier 2 alone would pick it", func(t *testing.T) {
		withNotification := "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{}}\n\n" +
			"data: {\"jsonrpc\":\"2.0\",\"id\":55,\"result\":{\"tools\":[\"decoy\"]}}\n\n" +
			"data: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{\"tools\":[\"right\"]}}\n\n"
		got := string(extractSSEResponse([]byte(withNotification), &wantID))
		if got != want {
			t.Errorf("extractSSEResponse (with notification) = %q, want %q", got, want)
		}
	})
}

// TestExtractSSEResponseNoResponseInStream is the regression test for the
// "last event" fallback tier that used to exist: if a stream never actually
// answers the request (no event matches wantID, and no event carries
// result/error — e.g. only a progress notification), extractSSEResponse must
// return the raw body rather than silently returning the notification's
// bytes as if they were the response. The old behavior made decodeMessage
// happily parse the notification into a zero-value {Result:nil, Error:nil}
// message, so call() returned (nil, nil) — the CLI treating "the server
// never answered" as a successful empty response. Returning the raw body
// instead makes json.Unmarshal fail visibly (it isn't valid JSON-RPC, or
// even valid JSON at all), surfacing the failure instead of hiding it.
func TestExtractSSEResponseNoResponseInStream(t *testing.T) {
	sse := "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"p\":50}}\n\n"
	got := extractSSEResponse([]byte(sse), nil)
	if string(got) != sse {
		t.Errorf("extractSSEResponse = %q, want the raw body %q (a notification-only stream must not be mistaken for a response)", got, sse)
	}
	if msg, err := decodeMessage(got); err == nil {
		t.Errorf("decodeMessage(%q) = %+v, <nil>, want a parse error (silent success bug: a notification decoded as an empty successful response)", got, msg)
	}

	// Same shape, but with a wantID set and no event anywhere carrying that
	// id or a result/error: must still fall through to the raw body, not to
	// whichever event happens to be last.
	wantID := 5
	got = extractSSEResponse([]byte(sse), &wantID)
	if string(got) != sse {
		t.Errorf("extractSSEResponse (wantID=5, no match) = %q, want raw body %q", got, sse)
	}
}

// TestExtractSSEResponseMultipleNotificationsNoResponse covers a stream with
// several events, none of which carry a result or error anywhere (not just a
// single-event stream) — still must surface as a visible failure rather than
// picking the last notification.
func TestExtractSSEResponseMultipleNotificationsNoResponse(t *testing.T) {
	sse := "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"p\":10}}\n\n" +
		"data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"p\":90}}\n\n"
	got := extractSSEResponse([]byte(sse), nil)
	if string(got) != sse {
		t.Errorf("extractSSEResponse = %q, want raw body %q", got, sse)
	}
	if _, err := decodeMessage(got); err == nil {
		t.Error("decodeMessage succeeded on a multi-notification stream with no response, want a visible parse error")
	}
}

// TestExtractSSEResponseSelectsNullOrEmptyResult are regression guards: a
// legitimate response carrying "result":null, "result":false, or
// "result":{} must still be selected as the response (all are valid,
// present `result` fields per JSON-RPC — a call that legitimately returns no
// data), not treated as missing just because they look "empty" or falsy.
func TestExtractSSEResponseSelectsNullOrEmptyResult(t *testing.T) {
	cases := []struct {
		name string
		sse  string
		want string
	}{
		{
			name: "null result",
			sse:  "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":null}\n\n",
			want: `{"jsonrpc":"2.0","id":1,"result":null}`,
		},
		{
			name: "false result",
			sse:  "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":false}\n\n",
			want: `{"jsonrpc":"2.0","id":1,"result":false}`,
		},
		{
			name: "empty object result",
			sse:  "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n",
			want: `{"jsonrpc":"2.0","id":1,"result":{}}`,
		},
		{
			name: "error instead of result",
			sse:  "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"error\":{\"code\":-32601,\"message\":\"nope\"}}\n\n",
			want: `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"nope"}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(extractSSEResponse([]byte(tc.sse), nil))
			if got != tc.want {
				t.Errorf("extractSSEResponse = %q, want %q (must still be selected as the response, not treated as missing)", got, tc.want)
			}
			// And it must decode as a legitimate message, with no error from
			// decodeMessage itself (the JSON is well-formed either way).
			if _, err := decodeMessage([]byte(got)); err != nil {
				t.Errorf("decodeMessage(%q) failed: %v", got, err)
			}
		})
	}
}

// TestExtractSSEResponseFallsBackToResultEventOnIDMismatch is a regression
// guard: when wantID doesn't match any event's id, tier 2 must still fall
// back to the event carrying result/error (a plain numeric id mismatch —
// this already worked before this package's most recent fixes; it must keep
// working after them).
func TestExtractSSEResponseFallsBackToResultEventOnIDMismatch(t *testing.T) {
	wantID := 42
	sse := "data: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{\"tools\":[]}}\n\n"
	want := `{"jsonrpc":"2.0","id":7,"result":{"tools":[]}}`
	got := string(extractSSEResponse([]byte(sse), &wantID))
	if got != want {
		t.Errorf("extractSSEResponse = %q, want %q (a plain id mismatch must still fall back to the result-bearing event)", got, want)
	}
}

// TestExtractSSEResponseSelectsResponseRegardlessOfIDShape guards against a
// real over-correction found in review of the "return raw body instead of
// the last event" fix above: tier 2 (the event-carrying-result/error scan)
// used to be gated on the event's id decoding as a non-null *int (idOf). A
// string id, or the JSON-RPC 2.0 spec-mandated `id: null`, failed that gate
// and fell through all the way to the raw-body return — discarding a
// legitimate, in the null case spec-*mandated*, response and replacing it
// with a confusing "parsing gateway response" decode failure instead of the
// server's actual answer.
//
// The null-id case is not theoretical: per
// https://www.jsonrpc.org/specification (Response object), "If there was an
// error in detecting the id in the Request object (e.g. Parse error/Invalid
// Request), it MUST be Null." A spec-compliant gateway answering a
// -32700/-32600 error is required to send exactly this shape.
//
// Presence of result/error alone is sufficient to identify a response: a
// notification carries method/params and never result/error, and neither
// does a server-initiated request, so no id-shape gate is needed to exclude
// them.
func TestExtractSSEResponseSelectsResponseRegardlessOfIDShape(t *testing.T) {
	wantID := 1

	t.Run("string id result", func(t *testing.T) {
		sse := "data: {\"jsonrpc\":\"2.0\",\"id\":\"1\",\"result\":{\"ok\":true}}\n\n"
		want := `{"jsonrpc":"2.0","id":"1","result":{"ok":true}}`
		got := string(extractSSEResponse([]byte(sse), &wantID))
		if got != want {
			t.Errorf("extractSSEResponse = %q, want %q (a string id must not disqualify a result-bearing event)", got, want)
		}
		if _, err := decodeMessage([]byte(got)); err != nil {
			t.Errorf("decodeMessage(%q) failed: %v", got, err)
		}
	})

	t.Run("null id error (JSON-RPC 2.0 spec-mandated shape for parse/invalid-request errors)", func(t *testing.T) {
		sse := "data: {\"jsonrpc\":\"2.0\",\"id\":null,\"error\":{\"code\":-32700,\"message\":\"parse error\"}}\n\n"
		want := `{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"parse error"}}`
		got := string(extractSSEResponse([]byte(sse), &wantID))
		if got != want {
			t.Errorf("extractSSEResponse = %q, want %q (id:null is spec-mandated for parse/invalid-request errors and must still be selected as the response)", got, want)
		}
		msg, err := decodeMessage([]byte(got))
		if err != nil {
			t.Fatalf("decodeMessage(%q) failed: %v", got, err)
		}
		if msg.Error == nil || msg.Error.Code == nil || *msg.Error.Code != -32700 {
			t.Errorf("decoded message = %+v, want Error.Code == -32700", msg)
		}
	})

	// Same null-id shape, but with wantID nil: tier 1 is skipped entirely in
	// that case, so tier 2 alone must still pick this up.
	t.Run("null id error, no wantID", func(t *testing.T) {
		sse := "data: {\"jsonrpc\":\"2.0\",\"id\":null,\"error\":{\"code\":-32600,\"message\":\"invalid request\"}}\n\n"
		want := `{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"invalid request"}}`
		got := string(extractSSEResponse([]byte(sse), nil))
		if got != want {
			t.Errorf("extractSSEResponse = %q, want %q", got, want)
		}
	})
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
	if msg.Error == nil || msg.Error.Code == nil || *msg.Error.Code != -32601 {
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

// TestRPCErrorCodePresenceVsAbsence pins the presence-tracking guarantee
// cmd/mcp_gateway.go's classifyGatewayError depends on: a JSON-RPC error
// object carrying a literal `"code":0` must decode distinguishably from one
// that omits the `code` key entirely -- the former is the observed shape of
// an upstream connector failure, the latter is just an error object without
// a code. Driven from raw JSON (not by constructing an rpcError by hand) so
// it actually exercises encoding/json's unmarshaling behavior, not an
// assumption about it.
func TestRPCErrorCodePresenceVsAbsence(t *testing.T) {
	msgZero, err := decodeMessage([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":0,"message":"boom"}}`))
	if err != nil {
		t.Fatalf("decode (code:0): %v", err)
	}
	if msgZero.Error == nil || msgZero.Error.Code == nil {
		t.Fatalf("code:0 must decode to a present, non-nil *int, got %+v", msgZero.Error)
	}
	if *msgZero.Error.Code != 0 {
		t.Errorf("code:0 must dereference to 0, got %d", *msgZero.Error.Code)
	}

	msgAbsent, err := decodeMessage([]byte(`{"jsonrpc":"2.0","id":1,"error":{"message":"boom"}}`))
	if err != nil {
		t.Fatalf("decode (no code key): %v", err)
	}
	if msgAbsent.Error == nil {
		t.Fatalf("expected a non-nil Error object")
	}
	if msgAbsent.Error.Code != nil {
		t.Errorf("an error object with no code key must decode to a nil *int (absent), got a present %d -- this is exactly the presence/zero collapse the fix exists to prevent", *msgAbsent.Error.Code)
	}

	// RPCErrorCode must propagate that same distinction to cmd/mcp_gateway.go.
	if code, ok := RPCErrorCode(msgZero.Error); !ok || code == nil || *code != 0 {
		t.Errorf("RPCErrorCode(code:0 error) = (%v, %v), want (non-nil *0, true)", code, ok)
	}
	if code, ok := RPCErrorCode(msgAbsent.Error); !ok || code != nil {
		t.Errorf("RPCErrorCode(code-absent error) = (%v, %v), want (nil, true)", code, ok)
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

// initializingGatewayServer builds an httptest server that answers "initialize"
// and "notifications/initialized" the normal way (issuing wantSession), and
// delegates "tools/list" to listTools so pagination tests only need to write
// the tools/list branch.
func initializingGatewayServer(t *testing.T, wantSession string, listTools func(w http.ResponseWriter, cursor string)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
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
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			cursor, _ := req.Params["cursor"].(string)
			w.Header().Set("Content-Type", "application/json")
			listTools(w, cursor)
		default:
			t.Errorf("unexpected method %q", req.Method)
		}
	}))
}

// TestListToolsThreePages verifies the legitimate multi-page path still
// returns the full union of tools when pagination spans three pages (not
// just the two TestEndToEnd already covers), now that ListTools also tracks
// seen cursors to guard against non-terminating pagination.
func TestListToolsThreePages(t *testing.T) {
	srv := initializingGatewayServer(t, "sess-3page", func(w http.ResponseWriter, cursor string) {
		switch cursor {
		case "":
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"a"}],"nextCursor":"p2"}}`)
		case "p2":
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"b"}],"nextCursor":"p3"}}`)
		case "p3":
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"c"}]}}`)
		default:
			t.Errorf("unexpected cursor %q", cursor)
		}
	})
	defer srv.Close()

	ctx := context.Background()
	c := New(srv.URL, "test-token", srv.Client())
	if err := c.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 3 || tools[0].Name != "a" || tools[1].Name != "b" || tools[2].Name != "c" {
		t.Errorf("ListTools = %+v, want [a b c] across three pages", tools)
	}
}

// TestListToolsRepeatedCursorTerminates is the regression test for a server
// that hands back the same non-empty cursor forever (with an empty tools
// array each time) — the exact pathological case that used to make ListTools
// spin indefinitely. It must terminate quickly (within a couple of pages,
// well under maxToolsListPages) via the seen-cursor guard, and surface a
// visible error rather than silently returning a partial/empty list.
func TestListToolsRepeatedCursorTerminates(t *testing.T) {
	var calls int
	srv := initializingGatewayServer(t, "sess-stuck", func(w http.ResponseWriter, cursor string) {
		calls++
		// Always the same cursor, always zero tools: pagination never
		// advances and never terminates on its own.
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[],"nextCursor":"stuck"}}`)
	})
	defer srv.Close()

	ctx := context.Background()
	c := New(srv.URL, "test-token", srv.Client())
	if err := c.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	_, err := c.ListTools(ctx)
	if err == nil {
		t.Fatal("ListTools returned a nil error against a repeated cursor, want a visible error instead of an infinite loop")
	}
	if calls > 3 {
		t.Errorf("tools/list was called %d times, want termination within a couple of pages on a repeated cursor", calls)
	}
	t.Logf("ListTools terminated after %d tools/list call(s) with error: %v", calls, err)
}

// TestListToolsMaxPagesBackstop verifies the absolute page-count backstop:
// a server that hands out a distinct, never-repeating cursor on every page
// (so the seen-cursor guard alone never fires) must still be stopped, by
// maxToolsListPages, rather than paginating forever. The var is lowered for
// the duration of the test so it stays fast.
func TestListToolsMaxPagesBackstop(t *testing.T) {
	orig := maxToolsListPages
	maxToolsListPages = 3
	defer func() { maxToolsListPages = orig }()

	var calls int
	srv := initializingGatewayServer(t, "sess-runaway", func(w http.ResponseWriter, cursor string) {
		calls++
		// A fresh cursor every call -- never repeats -- so only the
		// page-count backstop, not the seen-cursor guard, can stop this.
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[],"nextCursor":"page-%d"}}`, calls)
	})
	defer srv.Close()

	ctx := context.Background()
	c := New(srv.URL, "test-token", srv.Client())
	if err := c.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	_, err := c.ListTools(ctx)
	if err == nil {
		t.Fatalf("ListTools returned a nil error after %d calls against an ever-advancing cursor, want the max-page backstop to trigger", calls)
	}
	if calls > maxToolsListPages+1 {
		t.Errorf("tools/list was called %d times, want termination at or shortly after maxToolsListPages=%d", calls, maxToolsListPages)
	}
	t.Logf("ListTools terminated after %d tools/list call(s) with error: %v", calls, err)
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

// TestHTTPErrorNamesFailingRPCMethod verifies that a non-2xx response to a
// specific JSON-RPC call (tools/list here, distinct from the initialize call
// that must succeed first) is attributable to that call: HTTPError.RPCMethod
// names it, and the rendered Error() message both names it and still contains
// the response body — MCP being a single-endpoint protocol means Method/Path
// alone (always "POST" and the gateway path) can't tell initialize apart from
// tools/list apart from tools/call, which is the gap this closes.
func TestHTTPErrorNamesFailingRPCMethod(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &req)
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "sess")
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, "forbidden: insufficient scope")
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

	_, err := c.ListTools(ctx)
	if err == nil {
		t.Fatal("expected an error from a 403 tools/list response")
	}
	he, ok := err.(*HTTPError)
	if !ok {
		t.Fatalf("error type = %T, want *HTTPError", err)
	}
	if he.RPCMethod != "tools/list" {
		t.Errorf("RPCMethod = %q, want %q", he.RPCMethod, "tools/list")
	}
	msg := he.Error()
	if !strings.Contains(msg, "tools/list") {
		t.Errorf("Error() = %q, want it to name the failing RPC method %q", msg, "tools/list")
	}
	if !strings.Contains(msg, "forbidden: insufficient scope") {
		t.Errorf("Error() = %q, want it to still contain the response body", msg)
	}
}

// TestHTTPErrorClassificationAcrossStatuses verifies that adding RPCMethod to
// HTTPError did not disturb the errors.As(err, &apiErr) chain that
// cmd/errors.go's exitCode relies on to classify gateway failures: HTTPError
// must still unwrap to a *client.APIError carrying the original status code
// for every status that maps to a distinct exit code (401/403 -> auth,
// 404 -> not found, 429 -> rate limited, 5xx -> server error). This asserts
// the classified behavior via errors.As, not by inspecting the struct.
func TestHTTPErrorClassificationAcrossStatuses(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "boom")
			}))
			defer srv.Close()

			c := New(srv.URL, "test-token", srv.Client())
			err := c.Initialize(context.Background())
			if err == nil {
				t.Fatalf("expected an error for status %d", status)
			}

			var apiErr *client.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("errors.As(err, &apiErr) = false for status %d; err = %v (%T)", status, err, err)
			}
			if apiErr.StatusCode != status {
				t.Errorf("apiErr.StatusCode = %d, want %d", apiErr.StatusCode, status)
			}
		})
	}
}

// TestTransportFailureReturnsTransportError proves a failure that never
// produces an HTTP response at all -- here, a closed port, so the request is
// refused before any bytes come back -- is classified distinctly from
// HTTPError (a non-2xx status) and rpcError (a JSON-RPC-level error on a 200).
// Before this fix, c.httpClient.Do's error was returned bare, which collapsed
// to the generic exit code once wrapped in cmd; TransportError is the seam
// cmd/mcp_gateway.go's classifyGatewayError needs to route it to exit 8
// instead.
func TestTransportFailureReturnsTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := srv.URL
	srv.Close() // nothing is listening on this port now: connection refused

	c := New(closedURL, "test-token", nil)
	err := c.Initialize(context.Background())
	if err == nil {
		t.Fatal("expected an error against a closed port")
	}
	var transportErr *TransportError
	if !errors.As(err, &transportErr) {
		t.Fatalf("error = %T (%v), want *TransportError", err, err)
	}
	if transportErr.RPCMethod != methodInitialize {
		t.Errorf("RPCMethod = %q, want %q", transportErr.RPCMethod, methodInitialize)
	}

	// Negative pair: a bad status further down the same code path (HTTPError)
	// must not also satisfy TransportError -- it's a different failure class
	// (a real response arrived, just a non-2xx one).
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv2.Close()
	c2 := New(srv2.URL, "bad-token", srv2.Client())
	err2 := c2.Initialize(context.Background())
	var transportErr2 *TransportError
	if errors.As(err2, &transportErr2) {
		t.Errorf("an HTTP 403 response must not classify as *TransportError, got %v", err2)
	}
}

// TestAcceptHeaderAdvertisesBothMediaTypes pins the exact Accept header value
// the client sends. The C1 gateway has been observed rejecting a request that
// advertises only one of the two media types with 400 "Accept must contain
// both 'application/json' and 'text/event-stream'" — trimming this header to
// just one type is a cheap mistake for a future reader to make, and it only
// fails at runtime against a real gateway, not in any unit test that doesn't
// pin the header itself.
func TestAcceptHeaderAdvertisesBothMediaTypes(t *testing.T) {
	var gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-token", srv.Client())
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	want := "application/json, text/event-stream"
	if gotAccept != want {
		t.Errorf("Accept header = %q, want %q (both media types, or the C1 gateway returns 400)", gotAccept, want)
	}
}

// TestExtractSSEResponseIgnoresC1CommentPreamble pins a shape captured from
// C1's live gateway rather than inferred from the spec: the standalone GET
// stream on the MCP endpoint opens with a `: ok` SSE comment line followed by
// a blank line, then goes quiet. Comment lines (any line beginning with ':')
// carry no field and must be ignored, and the blank line after one must not be
// mistaken for the terminator of a real event. This client only issues POST
// today, so C1's own preamble is not reached in practice — the case is pinned
// because it is the one piece of real C1 SSE framing anyone has observed, and
// a parser that treated it as a field line or as an empty event would break on
// byte one if this client ever reads that stream.
//
// Scope, honestly: this is a documentation pin, not a strong guard. Breaking
// the `data:` prefix gate is caught by TestExtractSSEResponse, not by this
// test — id matching rescues this input even from a parser that mishandles
// comment lines. It is here so the one observed piece of real C1 SSE framing
// is recorded in the suite rather than only in a chat log.
func TestExtractSSEResponseIgnoresC1CommentPreamble(t *testing.T) {
	id := 2
	body := ": ok\n\n" +
		"data: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"tools\":[]}}\n\n"
	got := string(extractSSEResponse([]byte(body), &id))
	want := `{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}`
	if got != want {
		t.Errorf("comment preamble not ignored:\n got %q\nwant %q", got, want)
	}
}

// TestClientWorksWithoutSessionID pins that a gateway which never issues an
// Mcp-Session-Id still works end to end. The header is OPTIONAL in the MCP
// spec — a stateless server may omit it — and this client is deliberately
// tolerant of that: post only sets the header when one was captured.
//
// Live probing has since established that C1's gateway always DOES return a
// session id, and that it is functionally required there (calling tools/list
// before the handshake completes is rejected). So this path is spec compliance
// rather than an observed C1 mode — which is exactly why it needs a test: it
// is the one handshake behavior no real traffic will ever exercise for us, and
// a future "simplify" that made the header mandatory would break stateless
// servers with nothing to catch it.
//
// Recovered from an abandoned branch (commit 22f0c40) whose work was otherwise
// superseded; nothing on main pinned this.
func TestClientWorksWithoutSessionID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Mcp-Session-Id"); got != "" {
			t.Errorf("client sent Mcp-Session-Id %q despite the server never issuing one", got)
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("server got bad JSON: %v", err)
		}
		switch req.Method {
		case "initialize":
			// Deliberately no Mcp-Session-Id response header.
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18"}}`)
		case "notifications/initialized":
			if req.ID != nil {
				t.Errorf("notifications/initialized carried an id (%d); a notification must have none", *req.ID)
			}
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"tool_stateless","description":"d"}]}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", srv.Client())
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize against a session-less gateway: %v", err)
	}
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools against a session-less gateway: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "tool_stateless" {
		t.Errorf("ListTools = %+v, want exactly one tool named tool_stateless", tools)
	}
}

// TestExtractSSEResponseHostileFraming covers spec-legal SSE framing shapes
// that a compliant server may emit and that the other tests here don't reach:
// CRLF line endings, a data: field with no space after the colon, an event
// carrying only non-data fields, and a final event with no terminating blank
// line.
//
// These were originally written as throwaway probes during an adversarial
// review, run once, and discarded. They are durable regression coverage, so
// they are landed here rather than re-derived the next time someone touches
// the parser.
func TestExtractSSEResponseHostileFraming(t *testing.T) {
	const response = `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`

	for _, tc := range []struct {
		name string
		sse  string
	}{
		// SSE permits CRLF; bufio.Scanner's ScanLines strips the trailing \r,
		// so no stray carriage return may reach the JSON decoder.
		{"CRLF line endings", "data: " + response + "\r\n\r\n"},
		// The space after "data:" is optional, not required.
		{"no space after colon", "data:" + response + "\n\n"},
		// An event with event:/id: but no data: carries no payload and must be
		// dropped entirely rather than yielding an empty candidate.
		{"preceding event with no data field", "event: ping\nid: 5\n\ndata: " + response + "\n\n"},
		// A comment line (leading ':') is ignored by the SSE parser.
		{"preceding comment line", ": keepalive\n\ndata: " + response + "\n\n"},
		// A stream may end without a terminating blank line; the final event
		// must still be flushed.
		{"no trailing blank line", "data: " + response},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(extractSSEResponse([]byte(tc.sse), nil)); got != response {
				t.Errorf("extractSSEResponse = %q, want %q", got, response)
			}
		})
	}
}

// TestExtractSSEResponseEmptyDataLineIsNotAResponse pins that a stream whose
// only event is an empty data: field is treated as "no response arrived" — it
// falls back to the raw body so the caller's decode fails visibly, rather than
// yielding an empty payload that would decode as a successful empty result.
// That silent-success shape is the bug this fallback tier exists to prevent.
func TestExtractSSEResponseEmptyDataLineIsNotAResponse(t *testing.T) {
	sse := "data:\n\n"
	got := extractSSEResponse([]byte(sse), nil)
	if string(got) != sse {
		t.Errorf("extractSSEResponse = %q, want the raw body %q so the decode fails visibly", got, sse)
	}
	if _, err := decodeMessage(got); err == nil {
		t.Error("decodeMessage succeeded on an empty-data stream; a missing response must surface as an error")
	}
}

// TestCallToolSurfacesZeroCodeRPCError pins a shape observed live against C1:
// an UPSTREAM CONNECTOR failure — the external MCP server being unreachable, or
// a vendor API returning 401 mid-call — arrives as HTTP 200 carrying a JSON-RPC
// `error` whose `code` is **0**, not as an HTTP error and not as
// `result.isError`.
//
// Zero is Go's zero value, so the idiomatic-looking `err.Code != 0` guard, or
// any switch on code that treats 0 as "unset, probably fine", would silently
// drop a real failure — a vendor 401 would read as success. This client
// deliberately keys only on `error` being present, never on its code. The test
// exists so that stays true: adding a code check here is the plausible
// refactor that would reintroduce the bug.
func TestCallToolSurfacesZeroCodeRPCError(t *testing.T) {
	const upstream = `connector tool "pagerduty_oauth_list_teams": call failed: tool "list_teams" returned error: api error 401:`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &req)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "s1")
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18"}}`)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		default:
			// HTTP 200 with a JSON-RPC error carrying code 0.
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":2,"error":{"code":0,"message":`+
				strconvQuote(upstream)+`}}`)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", srv.Client())
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	_, err := c.CallTool(context.Background(), "pagerduty_oauth_list_teams", nil)
	if err == nil {
		t.Fatal("a JSON-RPC error with code 0 was treated as success; an upstream connector failure must surface as an error")
	}
	if !strings.Contains(err.Error(), "api error 401") {
		t.Errorf("error %q does not carry the upstream message", err)
	}
}

// strconvQuote renders s as a JSON string literal for embedding in a fixture.
func strconvQuote(s string) string { return strconv.Quote(s) }

// TestOutboundRPCMethods pins outboundRPCMethods (the actual list the request
// builders in Initialize/ListTools/CallTool reference) to the four
// spec-required methods this client is known to send today.
//
// This is not just a change-detector: cmd/mcp_gateway.go's classifyGatewayError
// maps JSON-RPC -32601 ("method not found") to exit 8, not exit 2 (usage),
// specifically BECAUSE this client only ever sends this fixed set of methods —
// meaning a caller can never trigger -32601 themselves, so it firing must be a
// protocol-version mismatch or a bug in this CLI, not a bad invocation (see the
// comment on classifyGatewayError). If this test fails, it's because that set
// just changed — e.g. a new outbound RPC method was added behind a flag or a
// server capability. That invalidates the "caller can't cause -32601" premise
// for the NEW method: a caller could now legitimately hit "method not found"
// for it (e.g. talking to an older gateway that predates it), and reporting
// that as exit 8 would send them chasing a CLI bug that isn't there. Before
// updating this test, go revisit classifyGatewayError in cmd/mcp_gateway.go
// and decide whether -32601 still belongs at exit 8 unconditionally, or needs
// to be conditioned on which method it names.
func TestOutboundRPCMethods(t *testing.T) {
	want := []string{
		methodInitialize,
		methodNotificationsInitialized,
		methodToolsList,
		methodToolsCall,
	}
	got := append([]string(nil), outboundRPCMethods...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("outboundRPCMethods = %v, want %v\n\n"+
			"This set changed. cmd/mcp_gateway.go's classifyGatewayError maps "+
			"JSON-RPC -32601 (\"method not found\") to exit 8 on the premise that "+
			"this client only ever sends a small fixed set of methods, so a "+
			"caller can never cause -32601 themselves. Adding (or removing) an "+
			"outbound method invalidates that premise for the changed method — "+
			"revisit classifyGatewayError in cmd/mcp_gateway.go before updating "+
			"this test's expected list.", got, want)
	}
}
