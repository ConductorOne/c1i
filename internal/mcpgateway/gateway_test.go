package mcpgateway

import "testing"

func TestExtractSSEData(t *testing.T) {
	sse := "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}\n\n"
	got := string(extractSSEData([]byte(sse)))
	want := `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`
	if got != want {
		t.Errorf("extractSSEData = %q, want %q", got, want)
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
