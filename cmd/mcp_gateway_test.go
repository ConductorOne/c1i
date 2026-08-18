package cmd

import "testing"

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
