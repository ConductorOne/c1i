package cmd

import (
	"strings"
	"testing"
)

// A repeated --user-id used to keep the last value silently.
func TestOwnerCommandsRefuseRepeatedUserID(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	cases := map[string][]string{
		"apps add-owner":            {"apps", "add-owner", "app1"},
		"apps remove-owner":         {"apps", "remove-owner", "app1"},
		"entitlements add-owner":    {"entitlements", "add-owner", "ent1", "--app-id", "app1"},
		"entitlements remove-owner": {"entitlements", "remove-owner", "ent1", "--app-id", "app1"},
		"accounts set-owner":        {"accounts", "set-owner", "au1", "--app-id", "app1"},
	}
	for name, base := range cases {
		t.Run(name, func(t *testing.T) {
			s := newMutServer(t, fixedReply(200, `{}`))
			args := append(append([]string{}, base...), "--user-id", "u1", "--user-id", "u2")
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.exit != exitUsage {
				t.Fatalf("exit = %d (%v), want %d", r.exit, r.err, exitUsage)
			}
			if !strings.Contains(r.stderr, "--user-id can be given once") && !strings.Contains(r.err.Error(), "can be given once") {
				t.Errorf("error does not say --user-id is single-valued: %v", r.err)
			}
			if n := len(s.requests()); n != 0 {
				t.Errorf("%d request(s) sent, want none", n)
			}
			// A single value still works afterwards (flag state resets).
			ok := runMutationCLI(t, s.srv, 0, nil, append(append([]string{}, base...), "--user-id", "u1")...)
			if ok.err != nil {
				t.Errorf("single --user-id failed: %v", ok.err)
			}
		})
	}
}
