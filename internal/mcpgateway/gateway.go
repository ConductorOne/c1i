// Package mcpgateway is a minimal client for the C1 MCP gateway, which speaks
// the Model Context Protocol over streamable HTTP (JSON-RPC 2.0 POSTed to a
// single endpoint). It exists so `c1i mcp gateway ...` can drive the same
// handshake an MCP host would — initialize, list tools, call a tool — to close
// the configure-then-verify loop without hand-rolling the protocol.
package mcpgateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
)

// protocolVersion is the MCP revision c1i negotiates. The gateway echoes its
// own supported version in the initialize result.
const protocolVersion = "2025-06-18"

// The fixed, spec-required JSON-RPC methods this client ever sends. Adding a
// new outbound method here (or anywhere else this client sends a method)
// invalidates the "-32601 can't be caller-caused" invariant that
// cmd/mcp_gateway.go's classifyGatewayError relies on to map "method not
// found" to exit 8 instead of exit 2 (usage) — see TestOutboundRPCMethods in
// gateway_test.go, which fails on any change to this set, and revisit that
// mapping before touching either.
const (
	methodInitialize               = "initialize"
	methodNotificationsInitialized = "notifications/initialized"
	methodToolsList                = "tools/list"
	methodToolsCall                = "tools/call"
)

// outboundRPCMethods is every method above, gathered so a test can assert
// against the actual set the code sends rather than a hand-maintained copy.
var outboundRPCMethods = []string{
	methodInitialize,
	methodNotificationsInitialized,
	methodToolsList,
	methodToolsCall,
}

// Client is a single-session MCP gateway client. Not safe for concurrent use;
// each command builds one, runs its handshake, and discards it.
type Client struct {
	endpoint   string
	token      string
	httpClient *http.Client
	sessionID  string
	nextID     int
}

// New returns a client targeting endpoint (the gateway's streamable-HTTP URL)
// authenticated with the given bearer token.
func New(endpoint, token string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{endpoint: endpoint, token: token, httpClient: httpClient}
}

// Tool is one entry from tools/list.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// HTTPError is returned when the gateway responds with a non-2xx status. It
// carries the status code so callers can classify it — e.g. map a 401/403 to an
// authentication failure (exit 3) rather than a generic error. It unwraps to a
// *client.APIError so it threads through the same exit-code taxonomy every
// other API failure in this CLI does (cmd/errors.go's exitCode), without
// losing the gateway-specific message (including the response body) that
// Error() renders.
//
// Method and Path are always "POST" and the fixed gateway endpoint path — MCP
// is a single-endpoint protocol, so those two fields alone can't tell a caller
// which JSON-RPC call failed (initialize vs tools/list vs tools/call). RPCMethod
// carries that JSON-RPC method name as additional context; it is not part of
// client.APIError (see Unwrap) and does not change Method/Path's meaning.
type HTTPError struct {
	StatusCode int
	Body       string
	Method     string
	Path       string
	RPCMethod  string
}

func (e *HTTPError) Error() string {
	if e.Body != "" {
		return fmt.Sprintf("gateway %s request returned %d: %s", e.RPCMethod, e.StatusCode, e.Body)
	}
	return fmt.Sprintf("gateway %s request returned %d", e.RPCMethod, e.StatusCode)
}

// Unwrap exposes a *client.APIError carrying the same status/method/path/body
// so errors.As(err, &apiErr) reaches it through any wrapping (fmt.Errorf with
// %w) the caller does on top of HTTPError.
func (e *HTTPError) Unwrap() error {
	return &client.APIError{
		Method:     e.Method,
		Path:       e.Path,
		StatusCode: e.StatusCode,
		Body:       e.Body,
	}
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int   `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// Code is a *int, not int: a JSON-RPC error object that omits `code` entirely
// must decode distinguishably from one carrying a literal `code:0` — the
// latter is the observed shape of an upstream connector failure (see
// RPCErrorCode), and collapsing "absent" into Go's int zero value would make
// the two indistinguishable.
type rpcError struct {
	Code    *int            `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	code := "absent"
	if e.Code != nil {
		code = strconv.Itoa(*e.Code)
	}
	if len(e.Data) > 0 {
		return fmt.Sprintf("MCP error %s: %s (%s)", code, e.Message, string(e.Data))
	}
	return fmt.Sprintf("MCP error %s: %s", code, e.Message)
}

// RPCErrorCode returns the JSON-RPC error code carried by err, if err is (or
// wraps) a JSON-RPC-level error the gateway returned. ok is false for a
// transport-level failure (e.g. *HTTPError — a non-2xx HTTP status) or any
// other error, letting a caller distinguish "the gateway answered with a
// JSON-RPC error" from "the request never got a JSON-RPC-shaped response at
// all." code is nil when the JSON-RPC error object omitted the `code` field
// entirely — distinguishable from a present code of 0 (ok is still true in
// both cases).
//
// This exists so cmd — which owns the process exit-code taxonomy and the
// types (like *usageError) some of those codes must map to — can react to
// specific JSON-RPC codes without internal/mcpgateway importing package cmd.
//
// Deliberately NOT an Unwrap() on rpcError to a *client.APIError: code 0 (the
// shape observed for an upstream connector failure — an unreachable external
// MCP server, a vendor API error, ...) arrives on an HTTP 200 response, so
// there is no real status to attach. An earlier version of this fix unwrapped
// to *client.APIError{StatusCode: 502} to reach exit 6 through the existing
// ">= 500" rule, but that fabricated status then rendered as a false "status"
// field in --error-format json — a claim about the wire that never happened.
// cmd/mcp_gateway.go's classifyGatewayError uses this accessor to wrap a
// present code of 0 (and -32601/-32700/-32600) in an *upstreamError
// (cmd/errors.go) instead: exit 8, no invented status anywhere.
func RPCErrorCode(err error) (code *int, ok bool) {
	var rpcErr *rpcError
	if errors.As(err, &rpcErr) {
		return rpcErr.Code, true
	}
	return nil, false
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

// Initialize performs the MCP handshake: the `initialize` request (capturing
// the server-assigned Mcp-Session-Id) followed by the `notifications/initialized`
// acknowledgement. It must run before ListTools/CallTool.
func (c *Client) Initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "c1i", "version": "dev"},
	}
	if _, err := c.call(ctx, methodInitialize, params); err != nil {
		return err
	}
	// A Mcp-Session-Id is optional per the MCP transport spec: a stateless
	// gateway may omit it, in which case subsequent requests simply carry no
	// session header. Only send the header when the server gave us one
	// (handled in post). In practice the C1 gateway has always returned one
	// on initialize, and a request sent before this handshake completes is
	// rejected ("method ... is invalid during session initialization") — so
	// this optional-header handling is spec compliance for a gateway mode
	// c1i hasn't observed, not dead code; the observed C1 behavior still
	// requires the initialize -> capture header -> notifications/initialized
	// -> tools/* order this method enforces.
	//
	// notifications/initialized has no id and expects no result.
	return c.notify(ctx, methodNotificationsInitialized)
}

// maxToolsListPages caps ListTools pagination as a backstop against a server
// that keeps handing out distinct, always-advancing cursors indefinitely. The
// per-cursor repeat guard in ListTools (below) already catches the more
// common failure — the same cursor (or an already-seen one) coming back
// forever — after just one extra page, but it can't catch a cursor that is
// always new. 1000 is generous for any real tool catalog (C1's gateway
// exposes a handful to a few hundred tools) while still bounding a runaway
// server to a finite, human-diagnosable number of requests rather than an
// unbounded hang. A var (not const) so tests can lower it to keep the
// pathological-pagination test fast.
var maxToolsListPages = 1000

// ListTools returns the tools the gateway exposes to the caller, following
// MCP cursor pagination (tools/list returns a nextCursor when more tools remain)
// so the full set is returned even when it spans multiple pages.
//
// It guards against a misbehaving server that never terminates pagination:
// a cursor that repeats (the same one twice, or one seen on an earlier page)
// and an absolute page-count backstop (maxToolsListPages) both stop the loop
// and return an error rather than either hanging forever or silently
// returning a truncated list — the latter would reintroduce, in a different
// shape, the exact silent-partial-success failure mode this client's SSE
// handling was already hardened against (see extractSSEResponse).
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	var all []Tool
	cursor := ""
	seen := map[string]bool{"": true} // "" is the initial (no-cursor) state
	for page := 0; ; page++ {
		if page >= maxToolsListPages {
			return nil, fmt.Errorf("tools/list did not terminate after %d pages (cursor %q); the gateway appears to be paginating without end", maxToolsListPages, cursor)
		}
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := c.call(ctx, methodToolsList, params)
		if err != nil {
			return nil, err
		}
		var out struct {
			Tools      []Tool `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, fmt.Errorf("parsing tools/list result: %w", err)
		}
		all = append(all, out.Tools...)
		if out.NextCursor == "" {
			return all, nil
		}
		if seen[out.NextCursor] {
			return nil, fmt.Errorf("tools/list returned a repeated cursor %q; the gateway is not making pagination progress", out.NextCursor)
		}
		seen[out.NextCursor] = true
		cursor = out.NextCursor
	}
}

// CallTool invokes name with the given arguments (raw JSON object, or nil for
// no arguments) and returns the raw MCP result (its `content` array, `isError`,
// etc.) for the caller to render.
func (c *Client) CallTool(ctx context.Context, name string, arguments json.RawMessage) (json.RawMessage, error) {
	args := arguments
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	params := map[string]any{"name": name, "arguments": args}
	return c.call(ctx, methodToolsCall, params)
}

// call sends a JSON-RPC request expecting a response, and returns its result.
func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.nextID++
	id := c.nextID
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	respBody, sessionID, err := c.post(ctx, method, body, &id)
	if err != nil {
		return nil, err
	}
	if sessionID != "" {
		c.sessionID = sessionID
	}
	msg, err := decodeMessage(respBody)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	if msg.Error != nil {
		return nil, msg.Error
	}
	return msg.Result, nil
}

// notify sends a JSON-RPC notification (no id, no response expected).
func (c *Client) notify(ctx context.Context, method string) error {
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: method})
	if err != nil {
		return err
	}
	_, _, err = c.post(ctx, method, body, nil)
	return err
}

// post sends one JSON-RPC payload to the gateway and returns the raw response
// body plus any Mcp-Session-Id header. It sets the session header on requests
// once known, and accepts both a JSON and an SSE response (per the streamable-
// HTTP transport, the server may answer either way). rpcMethod is the JSON-RPC
// method name being sent (e.g. "tools/list") — carried into HTTPError so a
// non-2xx response can be attributed to the RPC that triggered it, since
// Method/Path alone are always "POST" and the fixed gateway path. wantID is
// the id of the JSON-RPC request being sent (nil for a notification, which
// has none) — when the response arrives as an SSE stream with several
// events, it is used to pick out the event that answers this specific
// request.
func (c *Client) post(ctx context.Context, rpcMethod string, payload []byte, wantID *int) (body []byte, sessionID string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	// Both media types are required, not just advertised as a preference: the
	// C1 gateway rejects a request missing either with 400 "Accept must
	// contain both 'application/json' and 'text/event-stream'". Do not trim
	// this to just one type.
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", protocolVersion)
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", &HTTPError{
			StatusCode: resp.StatusCode,
			Body:       strings.TrimSpace(string(data)),
			Method:     req.Method,
			Path:       req.URL.Path,
			RPCMethod:  rpcMethod,
		}
	}

	sid := resp.Header.Get("Mcp-Session-Id")
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		data = extractSSEResponse(data, wantID)
	}
	return data, sid, nil
}

// decodeMessage parses a single JSON-RPC response object. An empty body (e.g. a
// 202 to a notification) decodes to an empty message with no error.
func decodeMessage(body []byte) (*rpcResponse, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return &rpcResponse{}, nil
	}
	var msg rpcResponse
	if err := json.Unmarshal(trimmed, &msg); err != nil {
		return nil, fmt.Errorf("parsing gateway response: %w (body: %s)", err, truncate(trimmed, 200))
	}
	return &msg, nil
}

// extractSSEResponse pulls the JSON-RPC response out of an SSE stream. A
// streamable-HTTP server may send several events in one response (e.g. progress
// notifications followed by the result), so concatenating every `data:` line
// would corrupt the JSON. It first parses the stream into events (blank-line
// separated); within an event, multiple `data:` lines are joined with "\n" per
// the SSE spec (https://html.spec.whatwg.org/multipage/server-sent-events.html),
// and exactly one optional leading space after the colon is stripped from each
// line — nothing else, so meaningful surrounding whitespace in the payload
// survives.
//
// It then picks the event that answers wantID, in order: (1) the event whose
// JSON-RPC id matches wantID, if wantID is non-nil; (2) the event carrying a
// `result` or `error`. Tier 2 deliberately does NOT require the event's id to
// parse as a non-null integer: a notification never carries `result`/`error`
// (it carries `method`/`params` instead), and neither does a server-initiated
// request, so the presence of `result`/`error` alone is already sufficient to
// identify a response — gating it on a parseable id as well would wrongly
// reject a response whose id is a string, or a response whose id is the JSON
// literal null. The latter is not a hypothetical: JSON-RPC 2.0 requires a
// null id specifically when the server could not determine the request's id
// — "If there was an error in detecting the id in the Request object (e.g.
// Parse error/Invalid Request), it MUST be Null." (jsonrpc.org/specification,
// Response object). So a spec-compliant -32700/-32600 error response is
// exactly the shape tier 2 must still select, not discard. If neither tier
// finds a match — e.g. the stream contains only progress notifications and
// never actually answers the request — the raw body is returned instead of
// guessing, the same as the scanner-error path below, so the caller's
// JSON-RPC decode fails visibly instead of a notification silently being
// mistaken for the response (which would make decodeMessage return a
// zero-value {Result:nil, Error:nil} message — i.e. the CLI treating "the
// server never answered" as a successful empty response). A scan error also
// returns the raw body, for the same reason.
//
// This handles the full input space the streamable-HTTP transport permits
// (multi-event streams, multi-line data fields, non-matching ids in-flight) —
// C1 has been observed answering every POST with a plain application/json
// body — never text/event-stream — across three independent capture sessions
// (including a 182s call, and including requests that did advertise
// text/event-stream in Accept, so the server had SSE on the table and
// declined it). C1 does emit real SSE framing on the standalone GET stream,
// whose first bytes are a `: ok` comment line, but this client only ever
// issues POST (see post), so that path is not reached here.
//
// So the multi-event path is spec-hardening against a mode this client
// advertises support for but has not observed on POST — not a fix for an
// observed C1 POST response shape. It is not merely theoretical either: the
// server does write SSE on another channel of the same endpoint.
func extractSSEResponse(body []byte, wantID *int) []byte {
	var events [][]byte
	var cur bytes.Buffer
	hasData := false // per-event: has at least one data: line been written to cur?
	flush := func() {
		if hasData {
			events = append(events, append([]byte(nil), cur.Bytes()...))
		}
		cur.Reset()
		hasData = false
	}
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" { // blank line terminates an SSE event
			flush()
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(line, "data:")
			data = strings.TrimPrefix(data, " ") // exactly one optional leading space, per spec
			if hasData {
				cur.WriteByte('\n') // multiple data: lines in one event join with \n, per spec
			}
			cur.WriteString(data)
			hasData = true
		}
	}
	flush()
	if sc.Err() != nil {
		return body
	}

	// idOf reports the event's JSON-RPC id, if the event decodes as an object
	// with a non-null "id" field. A notification has no id and always reports
	// ok=false, so it can never be selected as a response candidate below.
	idOf := func(e []byte) (id int, ok bool) {
		var probe struct {
			ID *int `json:"id"`
		}
		if json.Unmarshal(e, &probe) != nil || probe.ID == nil {
			return 0, false
		}
		return *probe.ID, true
	}

	if wantID != nil {
		for _, e := range events {
			if id, ok := idOf(e); ok && id == *wantID {
				return e
			}
		}
	}

	for _, e := range events {
		// No idOf gate here, deliberately: a string id or a spec-mandated
		// `id: null` (see the doc comment above) must still be selectable as
		// a response as long as it carries result/error. A notification and
		// a server-initiated request never carry either field, so this check
		// alone already excludes them without needing to inspect id at all.
		var probe struct {
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(e, &probe) == nil && (len(probe.Result) > 0 || len(probe.Error) > 0) {
			return e // the JSON-RPC response event
		}
	}
	// No event matched wantID and none carried result/error: this stream
	// never answers the request (e.g. notifications only). Returning the raw
	// body — rather than the last event, whatever it happens to be — makes
	// the caller's JSON-RPC decode fail visibly instead of silently reading
	// as success.
	return body
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
