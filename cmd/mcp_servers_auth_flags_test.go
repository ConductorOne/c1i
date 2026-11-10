package cmd

import (
	"strings"
	"testing"
)

// authArmCase pairs a --auth method with the companion flags it needs and a
// complete set of values for them.
type authArmCase struct {
	arm        string
	companions []string
	values     map[string]string
}

var authArmCases = []authArmCase{
	{"bearer-token", []string{"bearer-token"}, map[string]string{"bearer-token": "tok-123"}},
	{"custom-header", []string{"header-name", "header-value"}, map[string]string{"header-name": "X-Key", "header-value": "v-123"}},
	{"basic-auth", []string{"basic-auth-username", "basic-auth-password"}, map[string]string{"basic-auth-username": "user", "basic-auth-password": "pass"}},
}

// authFlagMutations are the commands that build an auth arm from the shared
// flags; both must refuse a blank companion before sending or previewing.
func authFlagMutations() map[string][]string {
	return map[string][]string{
		"register":           {"mcp", "servers", "register", "--app-id=app1", "--type=external", "--display-name=Srv", "--server-url=https://mcp.example.com/sse"},
		"update-credentials": {"mcp", "servers", "update-credentials", "cn1", "--app-id=app1", "--type=external"},
	}
}

// TestMcpAuthFlagsRequireCompanionValues pins that --auth with a blank
// companion value exits 2 before any request — including under --dry-run,
// whose preview used to print a request the server always 400s.
func TestMcpAuthFlagsRequireCompanionValues(t *testing.T) {
	for cmdName, base := range authFlagMutations() {
		for _, c := range authArmCases {
			for _, missing := range c.companions {
				for _, mode := range []string{"unset", "blank", "invisible"} {
					t.Run(cmdName+"/"+c.arm+"/"+mode+"/"+missing, func(t *testing.T) {
						args := append(append([]string{}, base...), "--auth="+c.arm)
						for _, n := range c.companions {
							if n == missing {
								switch mode {
								case "blank":
									args = append(args, "--"+n+"=")
								case "invisible":
									args = append(args, "--"+n+"=\u200b")
								}
								continue
							}
							args = append(args, "--"+n+"="+c.values[n])
						}

						withDryRun(t) // a preview must refuse too
						t.Setenv("C1I_URL", mutTestURL)
						s := newMutServer(t, fixedReply(200, `{"mcpServer":{"connectorId":"cn1"}}`))
						r := runMutationCLI(t, s.srv, 0, nil, args...)
						if r.exit != exitUsage {
							t.Fatalf("exit %d (%v), want %d", r.exit, r.err, exitUsage)
						}
						if r.err == nil || !strings.Contains(r.err.Error(), "--"+missing) {
							t.Errorf("err = %v, want it to name --%s", r.err, missing)
						}
						if got := len(s.requests()); got != 0 {
							t.Errorf("%d request(s) sent, want none", got)
						}
						if r.stdout != "" {
							t.Errorf("stdout = %q, want no preview", r.stdout)
						}
					})
				}
			}
		}
	}
}

// TestMcpAuthFlagsCompleteValuesPass pins the other direction: with every
// companion value supplied the arm is built, previewed and sent.
func TestMcpAuthFlagsCompleteValuesPass(t *testing.T) {
	for cmdName, base := range authFlagMutations() {
		for _, c := range authArmCases {
			t.Run(cmdName+"/"+c.arm+"/preview", func(t *testing.T) {
				args := append(append([]string{}, base...), "--auth="+c.arm)
				for _, n := range c.companions {
					args = append(args, "--"+n+"="+c.values[n])
				}
				withDryRun(t)
				t.Setenv("C1I_URL", mutTestURL)
				s := newMutServer(t, fixedReply(200, `{}`))
				r := runMutationCLI(t, s.srv, 0, nil, args...)
				if r.err != nil {
					t.Fatalf("exit %d: %v", r.exit, r.err)
				}
				for _, n := range c.companions {
					// The preview embeds the body pretty-printed.
					if !strings.Contains(r.stdout, `"`+jsonKeyFor(n)+`": "`+c.values[n]+`"`) {
						t.Errorf("preview missing %s value %q:\n%s", n, c.values[n], r.stdout)
					}
				}
				if got := len(s.requests()); got != 0 {
					t.Errorf("%d request(s) sent on --dry-run, want none", got)
				}
			})

			t.Run(cmdName+"/"+c.arm+"/send", func(t *testing.T) {
				args := append(append([]string{}, base...), "--auth="+c.arm)
				for _, n := range c.companions {
					args = append(args, "--"+n+"="+c.values[n])
				}
				t.Setenv("C1I_URL", mutTestURL)
				s := newMutServer(t, fixedReply(200, `{"mcpServer":{"connectorId":"cn1"}}`))
				r := runMutationCLI(t, s.srv, 0, nil, args...)
				if r.err != nil {
					t.Fatalf("exit %d: %v", r.exit, r.err)
				}
				reqs := s.requests()
				if len(reqs) != 1 {
					t.Fatalf("%d request(s), want 1", len(reqs))
				}
				for _, n := range c.companions {
					if !strings.Contains(reqs[0].body, `"`+jsonKeyFor(n)+`":"`+c.values[n]+`"`) {
						t.Errorf("body missing %s value %q: %s", n, c.values[n], reqs[0].body)
					}
				}
			})
		}
	}
}

// jsonKeyFor maps a companion flag to the proto field the body carries it in.
func jsonKeyFor(flag string) string {
	switch flag {
	case "bearer-token":
		return "token"
	case "header-name":
		return "headerName"
	case "header-value":
		return "headerValue"
	case "basic-auth-username":
		return "username"
	case "basic-auth-password":
		return "password"
	}
	return flag
}
