package mcpgateway

import (
	"strings"
	"testing"
)

func TestGatewayErrorsRedactEchoedURLs(t *testing.T) {
	code := -32000
	for name, err := range map[string]error{
		"http":     &HTTPError{StatusCode: 400, RPCMethod: "tools/call", Body: `{"message":"bad /x?token=SEKG1&keep=1"}`},
		"rpc":      &rpcError{Code: &code, Message: "bad https://h.example/x;token=SEKG1?keep=1"},
		"rpc data": &rpcError{Code: &code, Message: "bad /y?token=SEKG1&keep=1", Data: []byte(`{"u":"/x?api_key=SEKG1&keep=1"}`)},
	} {
		got := err.Error()
		if strings.Contains(got, "SEKG1") || !strings.Contains(got, "keep=1") {
			t.Errorf("%s: %q", name, got)
		}
	}
	if _, err := decodeMessage([]byte("oops /x?token=SEKG1&keep=1")); err == nil || strings.Contains(err.Error(), "SEKG1") || !strings.Contains(err.Error(), "keep=1") {
		t.Errorf("decodeMessage err = %v", err)
	}
}
