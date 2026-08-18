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
func TestExtractSSEResponseSelectsByRequestID(t *testing.T) {
	sse := "data: {\"jsonrpc\":\"2.0\",\"id\":99,\"result\":{\"tools\":[\"wrong\"]}}\n\n" +
		"data: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{\"tools\":[\"right\"]}}\n\n"
	wantID := 7
	got := string(extractSSEResponse([]byte(sse), &wantID))
	want := `{"jsonrpc":"2.0","id":7,"result":{"tools":["right"]}}`
	if got != want {
		t.Errorf("extractSSEResponse = %q, want %q (should select the event matching the request id)", got, want)
	}

	// The reverse order must select the same way — it's the id, not order.
	sseReversed := "data: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{\"tools\":[\"right\"]}}\n\n" +
		"data: {\"jsonrpc\":\"2.0\",\"id\":99,\"result\":{\"tools\":[\"wrong\"]}}\n\n"
	got = string(extractSSEResponse([]byte(sseReversed), &wantID))
	if got != want {
		t.Errorf("extractSSEResponse (reversed) = %q, want %q", got, want)
	}

	// A notification (no id) alongside the wanted response must never be
	// mistaken for the response, even without id matching.
	withNotification := "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{}}\n\n" +
		"data: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{\"tools\":[\"right\"]}}\n\n"
	got = string(extractSSEResponse([]byte(withNotification), &wantID))
	if got != want {
		t.Errorf("extractSSEResponse (with notification) = %q, want %q", got, want)
	}
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
		if msg.Error == nil || msg.Error.Code != -32700 {
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
