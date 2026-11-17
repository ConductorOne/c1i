package cmd

import (
	"strings"
	"testing"
)

// findings create-task --policy-id binds a referenced policy, so it is looked
// up first like every other policy binding: unknown 4, deleted 2, no type check.
func TestFindingsCreateTaskPolicyRef(t *testing.T) {
	policies := map[string]string{
		"gp": "POLICY_TYPE_GRANT", "rp": "POLICY_TYPE_REVOKE", "del": "deleted:POLICY_TYPE_GRANT",
		"np": "nopolicy", "bj": "badjson",
	}
	cases := []struct {
		name      string
		policy    string
		wantExit  int
		wantErr   string
		wantPosts int
	}{
		{name: "unknown", policy: "nope", wantExit: exitNotFound, wantErr: `--policy-id "nope": no such policy`},
		{name: "deleted", policy: "del", wantExit: exitUsage, wantErr: "policy is deleted"},
		{name: "known grant policy", policy: "gp", wantPosts: 1},
		{name: "known policy of another type", policy: "rp", wantPosts: 1},
		{name: "200 without a policy", policy: "np", wantExit: exitServer, wantErr: "no policy"},
		{name: "200 non-JSON", policy: "bj", wantExit: exitServer, wantErr: "parsing policy response"},
	}
	for _, tc := range cases {
		for _, dry := range []bool{false, true} {
			name := tc.name
			if dry {
				name += " dry-run"
			}
			t.Run(name, func(t *testing.T) {
				t.Setenv("C1I_URL", mutTestURL)
				clearViperOverrides(t, "dry_run") // another test's override would hide --dry-run
				srv, requests := policyBindingServer(t, policies)
				args := []string{"findings", "create-task", "f1", "--policy-id", tc.policy}
				if dry {
					args = append(args, "--dry-run")
				}
				res := runMutationCLI(t, srv, 0, nil, args...)
				if res.exit != tc.wantExit {
					t.Fatalf("exit = %d (err %v), want %d", res.exit, res.err, tc.wantExit)
				}
				if tc.wantErr != "" && (res.err == nil || !strings.Contains(res.err.Error(), tc.wantErr)) {
					t.Errorf("err = %v, want it to contain %q", res.err, tc.wantErr)
				}
				got := requests()
				if n := countRequests(got, "GET /api/v1/policies/"); n != 1 {
					t.Errorf("policy lookups = %d, want 1 (%v)", n, got)
				}
				wantPosts := tc.wantPosts
				if dry {
					wantPosts = 0
				}
				if n := countRequests(got, "POST "); n != wantPosts {
					t.Errorf("POSTs = %d, want %d (%v)", n, wantPosts, got)
				}
			})
		}
	}
}

// Without --policy-id nothing is looked up, so a preview still needs no
// credentials.
func TestFindingsCreateTaskWithoutPolicyDoesNoLookup(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	srv, requests := policyBindingServer(t, nil)
	res := runMutationCLI(t, srv, 0, nil, "findings", "create-task", "f1")
	if res.exit != 0 {
		t.Fatalf("exit = %d (err %v)", res.exit, res.err)
	}
	if n := countRequests(requests(), "GET "); n != 0 {
		t.Errorf("lookups = %d, want 0", n)
	}
}

func TestFindingsCreateTaskHelpStatesPolicyRead(t *testing.T) {
	if strings.Count(findingsCreateTaskCmd.Long, policyRefHelp) != 1 {
		t.Errorf("Long does not carry policyRefHelp once:\n%s", findingsCreateTaskCmd.Long)
	}
}
