package cmd

import (
	"strings"
	"testing"
)

// <trust-id> may be the part before "@"; a body clientId naming the same trust
// is accepted in either form, and a different one is refused.
func TestTrustsUpdateRefusesForeignClientID(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	const full = "t-1@tenant.example.com/wfe"
	cases := []struct {
		name string
		id   string
		body string
		exit int
	}{
		{"full equals full", full, `{"clientId":"` + full + `","displayName":"zz"}`, 0},
		{"short positional, full body", "t-1", `{"clientId":"` + full + `","displayName":"zz"}`, 0},
		{"foreign id", full, `{"clientId":"other@tenant.example.com/wfe","displayName":"zz"}`, exitUsage},
		{"snake spelling foreign", full, `{"client_id":"other@tenant.example.com/wfe","displayName":"zz"}`, exitUsage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newMutServer(t, fixedReply(200, `{}`))
			r := runMutationCLI(t, s.srv, 0, nil, "service-principals", "trusts", "update", tc.id,
				"--service-principal-id", "sp-1", "--body-file", writeTempFile(t, tc.body), "--update-mask", "displayName")
			if r.exit != tc.exit {
				t.Fatalf("exit %d (want %d): %s", r.exit, tc.exit, r.stderr)
			}
			if tc.exit != 0 && !strings.Contains(r.stderr, "client") {
				t.Errorf("stderr %q does not name clientId", r.stderr)
			}
		})
	}
}
