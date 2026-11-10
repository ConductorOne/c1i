package cmd

import (
	"strings"
	"testing"
)

// --body-file "" used to read as "no body file": the flags built the request,
// or a generic error (exit 1) asked for --display-name.
func TestPoliciesCreateEmptyBodyFileIsReadError(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"alone", []string{"policies", "create", "--body-file", ""}},
		{"with flags", []string{"policies", "create", "--body-file", "", "--display-name", "p", "--policy-type", "grant"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			srv := newMutServer(t, fixedReply(200, "{}"))
			res := runMutationCLI(t, srv.srv, 0, nil, tc.args...)
			if got := len(srv.requests()); got != 0 {
				t.Fatalf("requests sent = %d, want 0", got)
			}
			if res.exit != exitUsage {
				t.Fatalf("exit = %d (err %v), want %d", res.exit, res.err, exitUsage)
			}
			if tc.name == "alone" && (res.err == nil || !strings.Contains(res.err.Error(), "reading --body-file")) {
				t.Fatalf("err = %v, want it to contain %q", res.err, "reading --body-file")
			}
		})
	}
}
