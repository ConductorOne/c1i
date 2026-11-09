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

func TestMapperCasesElementsMustBeObjects(t *testing.T) {
	for _, dry := range []bool{false, true} {
		t.Run(fmt.Sprintf("dry=%v", dry), func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			srv := newMutServer(t, fixedReply(200, `{}`))
			f := writeTempFile(t, `{"mappingCases":[{"userKeyCel":"a","appUserKeyCel":"b"}, 1]}`)
			args := []string{"apps", "update", "app-1", "--app-user-mapper-file", f}
			if dry {
				args = append(args, "--dry-run")
			}
			res := runMutationCLI(t, srv.srv, 0, nil, args...)
			want := "--app-user-mapper-file mappingCases element 1 must be a JSON object, not a number"
			if res.exit != exitUsage || !strings.Contains(res.stderr, want) {
				t.Errorf("exit %d, stderr %q; want exit %d naming %q", res.exit, res.stderr, exitUsage, want)
			}
			if n := len(srv.requests()); n != 0 {
				t.Errorf("sent %d requests, want 0", n)
			}
		})
	}
}

// A policySteps value or its steps in the wrong container shape is refused,
// not skipped.
func TestBodyFilePolicyStepsShape(t *testing.T) {
	cases := []struct{ name, steps, want string }{
		{"entry string", `"x"`, `--body-file policySteps["grant"] must be a JSON object, not a string`},
		{"entry null", `null`, `--body-file policySteps["grant"] must be a JSON object, not null`},
		{"steps object", `{"steps":{}}`, `--body-file policySteps["grant"].steps must be a JSON array, not an object`},
		{"steps string", `{"steps":"x"}`, `--body-file policySteps["grant"].steps must be a JSON array, not a string`},
	}
	for _, tc := range cases {
		for _, cmdArgs := range [][]string{{"policies", "create"}, {"policies", "update", "p1", "--update-mask", "policySteps"}} {
			for _, dry := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/dry=%v", tc.name, cmdArgs[1], dry), func(t *testing.T) {
					t.Setenv("C1I_URL", mutTestURL)
					srv := newMutServer(t, fixedReply(200, `{}`))
					body := `{"displayName":"x","policyType":"POLICY_TYPE_GRANT","policySteps":{"grant":` + tc.steps + `}}`
					args := append(append([]string{}, cmdArgs...), "--body-file", writeTempFile(t, body))
					if dry {
						args = append(args, "--dry-run")
					}
					res := runMutationCLI(t, srv.srv, 0, nil, args...)
					if res.exit != exitUsage || !strings.Contains(res.stderr, tc.want) {
						t.Errorf("exit %d, stderr %q; want exit %d naming %q", res.exit, res.stderr, exitUsage, tc.want)
					}
					if n := len(srv.requests()); n != 0 {
						t.Errorf("sent %d requests, want 0", n)
					}
				})
			}
		}
	}
}

// TestBodyFilePolicyStepsNonBaselineEntry pins the one shape 0.9.2 sent: a
// non-object entry under a key other than the policy type's own.
func TestBodyFilePolicyStepsNonBaselineEntry(t *testing.T) {
	const want = `--body-file policySteps["revoke"] must be a JSON object, not a string`
	body := `{"displayName":"x","policyType":"POLICY_TYPE_GRANT","policySteps":{"grant":{"steps":[{"approval":{"users":{"userIds":["u1"]}}}]},"revoke":"x"}}`
	for _, cmdArgs := range [][]string{{"policies", "create"}, {"policies", "update", "p1", "--update-mask", "policySteps"}} {
		for _, dry := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry=%v", cmdArgs[1], dry), func(t *testing.T) {
				t.Setenv("C1I_URL", mutTestURL)
				srv := newMutServer(t, fixedReply(200, `{}`))
				args := append(append([]string{}, cmdArgs...), "--body-file", writeTempFile(t, body))
				if dry {
					args = append(args, "--dry-run")
				}
				res := runMutationCLI(t, srv.srv, 0, nil, args...)
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

func TestRulesFileFromStdinElementsMustBeObjects(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	srv := newMutServer(t, fixedReply(200, `{}`))
	steps := writeTempFile(t, `[{"approval":{"users":{"userIds":["u1"]}}}]`)
	policiesCreateCmd.SetIn(strings.NewReader(`[null]`))
	t.Cleanup(func() { policiesCreateCmd.SetIn(nil) })
	res := runMutationCLI(t, srv.srv, 0, nil, "policies", "create", "--display-name", "x", "--policy-type", "grant", "--steps-file", steps, "--rules-file", "-")
	if want := "--rules-file element 0 must be a JSON object, not null"; res.exit != exitUsage || !strings.Contains(res.stderr, want) {
		t.Errorf("exit %d, stderr %q; want exit %d naming %q", res.exit, res.stderr, exitUsage, want)
	}
	if n := len(srv.requests()); n != 0 {
		t.Errorf("sent %d requests, want 0", n)
	}
}

// Valid arrays still go through: exactly one request per command.
func TestValidArrayFilesStillSend(t *testing.T) {
	steps := `[{"approval":{"users":{"userIds":["u1"]}}}]`
	rules := `[{"condition":"true","stepKey":"grant"}]`
	refs := `[{"appId":"app-1","id":"ent-1"}]`
	cases := []struct {
		name string
		argv func(t *testing.T) []string
		reqs int
	}{
		{"create steps+rules", func(t *testing.T) []string {
			return []string{"policies", "create", "--display-name", "x", "--policy-type", "grant", "--steps-file", writeTempFile(t, steps), "--rules-file", writeTempFile(t, rules)}
		}, 1},
		{"update steps", func(t *testing.T) []string {
			return []string{"policies", "update", "p1", "--policy-type", "grant", "--steps-file", writeTempFile(t, steps)}
		}, 1},
		{"update rules", func(t *testing.T) []string {
			return []string{"policies", "update", "p1", "--rules-file", writeTempFile(t, rules)}
		}, 1},
		{"bundle-automation run", func(t *testing.T) []string {
			return []string{"access-profiles", "bundle-automation", "run", "ap1", "--refs-file", writeTempFile(t, refs)}
		}, 1},
		{"requestable-entitlements set", func(t *testing.T) []string {
			return []string{"access-profiles", "requestable-entitlements", "set", "ap1", "--refs-file", writeTempFile(t, refs)}
		}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			srv := newMutServer(t, fixedReply(200, `{}`))
			res := runMutationCLI(t, srv.srv, 0, nil, tc.argv(t)...)
			if res.exit != 0 {
				t.Fatalf("exit %d, stderr %q", res.exit, res.stderr)
			}
			if n := len(srv.requests()); n != tc.reqs {
				t.Errorf("sent %d requests, want %d", n, tc.reqs)
			}
		})
	}
}
