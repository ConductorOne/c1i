package cmd

import (
	"strings"
	"testing"
)

// The server trims a path segment before resolving dots, so a padded dot
// segment in a POSITIONAL reaches the collection just like an unpadded one.
// The transport guard has to apply the same trim the body/flag rule does.
func TestPaddedDotSegmentPositionalRefused(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"apps get", []string{"apps", "get", " .. "}},
		{"mcp tools approve", []string{"mcp", "tools", "approve", " .. ", "--app-id", "app-1", "--connector-id", "conn-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			srv := newMutServer(t, fixedReply(200, `{}`))
			res := runMutationCLI(t, srv.srv, 0, nil, tc.args...)
			if res.exit != exitUsage {
				t.Fatalf("exit = %d, err = %v; want %d", res.exit, res.err, exitUsage)
			}
			if res.err == nil || !strings.Contains(displayError(res.err).Error(), "refusing to send") {
				t.Errorf("err = %v, want the path-guard refusal", res.err)
			}
			if got := srv.requests(); len(got) != 0 {
				t.Errorf("requests sent: %v", got)
			}
		})
	}
}

// The trim must not widen the rule: a real id with surrounding spaces is sent
// (and the server rejects it on its own), not refused as a dot segment.
func TestPaddedNonDotPositionalIsNotRefused(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	srv := newMutServer(t, fixedReply(200, `{"app":{"id":"app-1"}}`))
	res := runMutationCLI(t, srv.srv, 0, nil, "apps", "get", " a.b ")
	if res.exit != 0 {
		t.Fatalf("exit = %d, err = %v; want 0", res.exit, res.err)
	}
	got := srv.requests()
	if len(got) != 1 {
		t.Fatalf("requests = %v, want 1", got)
	}
	if !strings.Contains(got[0].rawPath, "%20a.b%20") {
		t.Errorf("path = %q, want the escaped padded id", got[0].rawPath)
	}
}
