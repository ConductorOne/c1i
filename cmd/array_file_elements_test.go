package cmd

import (
	"fmt"
	"strings"
	"testing"
)

// Every array --*-file flag takes a JSON array of objects; readJSONFile
// refuses any other element before a request is built.
func TestArrayFileElementsMustBeObjects(t *testing.T) {
	const steps = `[{"approval":{"users":{"userIds":["u1"]}}}]`
	type pair struct {
		name, flag string
		argv       func(t *testing.T, file string) []string
	}
	pairs := []pair{
		{"policies create steps", "--steps-file", func(t *testing.T, f string) []string {
			return []string{"policies", "create", "--display-name", "x", "--policy-type", "grant", "--steps-file", f}
		}},
		{"policies create rules", "--rules-file", func(t *testing.T, f string) []string {
			return []string{"policies", "create", "--display-name", "x", "--policy-type", "grant", "--steps-file", writeTempFile(t, steps), "--rules-file", f}
		}},
		{"policies update steps", "--steps-file", func(t *testing.T, f string) []string {
			return []string{"policies", "update", "p1", "--policy-type", "grant", "--steps-file", f}
		}},
		{"policies update rules", "--rules-file", func(t *testing.T, f string) []string {
			return []string{"policies", "update", "p1", "--rules-file", f}
		}},
		{"bundle-automation run", "--refs-file", func(t *testing.T, f string) []string {
			return []string{"access-profiles", "bundle-automation", "run", "ap1", "--refs-file", f}
		}},
		{"requestable-entitlements set", "--refs-file", func(t *testing.T, f string) []string {
			return []string{"access-profiles", "requestable-entitlements", "set", "ap1", "--refs-file", f}
		}},
	}
	inputs := []struct{ json, idx, kind string }{
		{`[null]`, "0", "null"},
		{`[{"valid":1}, null]`, "1", "null"},
		{`["x"]`, "0", "a string"},
		{`[1]`, "0", "a number"},
		{`[[]]`, "0", "an array"},
		{`[true]`, "0", "a boolean"},
	}
	for _, p := range pairs {
		for _, in := range inputs {
			for _, dry := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/dry=%v", p.name, in.json, dry), func(t *testing.T) {
					t.Setenv("C1I_URL", mutTestURL)
					srv := newMutServer(t, fixedReply(200, `{}`))
					args := p.argv(t, writeTempFile(t, in.json))
					if dry {
						args = append(args, "--dry-run")
					}
					res := runMutationCLI(t, srv.srv, 0, nil, args...)
					want := fmt.Sprintf("%s element %s must be a JSON object, not %s", p.flag, in.idx, in.kind)
					if res.exit != exitUsage || !strings.Contains(res.stderr, want) {
						t.Errorf("exit %d, stderr %q; want exit %d naming %q", res.exit, res.stderr, exitUsage, want)
					}
					if n := len(srv.requests()); n != 0 {
						t.Errorf("sent %d requests, want 0", n)
					}
				})
			}
		}
	}
}

// An array nested in a --body-file follows the same rule, named by its
// location in the body.
func TestBodyFileNestedArrayElementsMustBeObjects(t *testing.T) {
	const valid = `{"approval":{"users":{"userIds":["u1"]}}}`
	policy := func(steps, rules string) string {
		return `{"displayName":"x","policyType":"POLICY_TYPE_GRANT","policySteps":{"grant":{"steps":` + steps + `}},"rules":` + rules + `}`
	}
	cases := []struct {
		name  string
		argv  []string
		body  string
		label string
		idx   string
		kind  string
	}{
		{"create rules null", []string{"policies", "create"}, policy("["+valid+"]", `[null]`), "--body-file rules", "0", "null"},
		{"create rules string at 1", []string{"policies", "create"}, policy("["+valid+"]", `[{"condition":"true","stepKey":"grant"},"x"]`), "--body-file rules", "1", "a string"},
		{"create steps null", []string{"policies", "create"}, policy(`[null]`, `[]`), `--body-file policySteps["grant"].steps`, "0", "null"},
		{"update rules null", []string{"policies", "update", "p1"}, policy("["+valid+"]", `[null]`), "--body-file rules", "0", "null"},
		{"update steps number", []string{"policies", "update", "p1"}, policy(`[1]`, `[]`), `--body-file policySteps["grant"].steps`, "0", "a number"},
		{"bundle-automation create refs null", []string{"access-profiles", "bundle-automation", "create", "ap1"}, `{"entitlements":{"entitlementRefs":[null]}}`, "--body-file entitlements.entitlementRefs", "0", "null"},
		{"bundle-automation set refs array", []string{"access-profiles", "bundle-automation", "set", "ap1"}, `{"entitlements":{"entitlementRefs":[[]]}}`, "--body-file entitlements.entitlementRefs", "0", "an array"},
	}
	for _, tc := range cases {
		for _, dry := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry=%v", tc.name, dry), func(t *testing.T) {
				t.Setenv("C1I_URL", mutTestURL)
				srv := newMutServer(t, fixedReply(200, `{}`))
				args := append(append([]string{}, tc.argv...), "--body-file", writeTempFile(t, tc.body))
				if tc.argv[1] == "update" {
					args = append(args, "--update-mask", "rules,policySteps")
				}
				if dry {
					args = append(args, "--dry-run")
				}
				res := runMutationCLI(t, srv.srv, 0, nil, args...)
				want := fmt.Sprintf("%s element %s must be a JSON object, not %s", tc.label, tc.idx, tc.kind)
				if res.exit != exitUsage || !strings.Contains(res.stderr, want) {
					t.Errorf("exit %d, stderr %q; want exit %d naming %q", res.exit, res.stderr, exitUsage, want)
				}
				if n := len(srv.requests()); n != 0 {
					t.Errorf("sent %d requests, want 0", n)
				}
			})
		}
	}
}
