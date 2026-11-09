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
