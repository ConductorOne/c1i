package cmd

import (
	"encoding/json"
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

// authMutation is a command that builds an auth arm from the shared flags,
// with the reply that makes it succeed.
type authMutation struct {
	args  []string
	reply string
}

func authFlagMutations() map[string]authMutation {
	return map[string]authMutation{
		"register":           {[]string{"mcp", "servers", "register", "--app-id=app1", "--type=external", "--display-name=Srv", "--server-url=https://mcp.example.com/sse"}, `{"mcpServer":{"connectorId":"cn1"}}`},
		"update-credentials": {[]string{"mcp", "servers", "update-credentials", "cn1", "--app-id=app1", "--type=external"}, `{"mcpServer":{"connectorId":"cn1"}}`},
		"test-connection":    {[]string{"mcp", "servers", "test-connection", "--server-url=https://mcp.example.com/sse"}, `{"reachable":true}`},
	}
}

// TestMcpAuthFlagsRequireCompanionValues pins that --auth with a blank
// companion value exits 2 before any request when the full arm is supplied —
// including under --dry-run, whose preview used to print a request the server
// always 400s.
func TestMcpAuthFlagsRequireCompanionValues(t *testing.T) {
	for cmdName, m := range authFlagMutations() {
		for _, c := range authArmCases {
			for _, missing := range c.companions {
				for _, mode := range []string{"unset", "blank", "invisible"} {
					t.Run(cmdName+"/"+c.arm+"/"+mode+"/"+missing, func(t *testing.T) {
						args := append(append([]string{}, m.args...), "--auth="+c.arm)
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
						s := newMutServer(t, fixedReply(200, m.reply))
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
	for cmdName, m := range authFlagMutations() {
		for _, c := range authArmCases {
			t.Run(cmdName+"/"+c.arm+"/send", func(t *testing.T) {
				args := append(append([]string{}, m.args...), "--auth="+c.arm)
				for _, n := range c.companions {
					args = append(args, "--"+n+"="+c.values[n])
				}
				t.Setenv("C1I_URL", mutTestURL)
				s := newMutServer(t, fixedReply(200, m.reply))
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

	// test-connection's preview masks secrets, so it is covered by
	// TestTestConnectionDryRunSendsNothingAndRedactsSecrets.
	for cmdName, m := range authFlagMutations() {
		if cmdName == "test-connection" {
			continue
		}
		for _, c := range authArmCases {
			t.Run(cmdName+"/"+c.arm+"/preview", func(t *testing.T) {
				args := append(append([]string{}, m.args...), "--auth="+c.arm)
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
					want := c.values[n]
					if secretBodyKeys[secretKey(jsonKeyFor(n))] {
						want = redactedPlaceholder // every preview masks credentials
					}
					if !strings.Contains(r.stdout, `"`+jsonKeyFor(n)+`": "`+want+`"`) {
						t.Errorf("preview missing %s value %q:\n%s", n, c.values[n], r.stdout)
					}
				}
				if got := len(s.requests()); got != 0 {
					t.Errorf("%d request(s) sent on --dry-run, want none", got)
				}
			})
		}
	}
}

// TestMcpAuthPerUserLeavesSecretToTheUser pins that --token-sharing per-user
// no longer requires the secret companions: the server expects them empty and
// the user supplies them later via SubmitUserCredential. A custom header's
// name is not a secret and is still required.
func TestMcpAuthPerUserLeavesSecretToTheUser(t *testing.T) {
	perUserArms := []authArmCase{
		{"bearer-token", []string{"bearer-token"}, nil},
		{"basic-auth", []string{"basic-auth-username", "basic-auth-password"}, nil},
		{"custom-header", []string{"header-value"}, map[string]string{"header-name": "X-Key"}},
	}
	for cmdName, m := range authFlagMutations() {
		for _, c := range perUserArms {
			t.Run(cmdName+"/"+c.arm, func(t *testing.T) {
				args := append(append([]string{}, m.args...), "--auth="+c.arm, "--token-sharing=per-user")
				for n, v := range c.values {
					args = append(args, "--"+n+"="+v)
				}
				t.Setenv("C1I_URL", mutTestURL)
				s := newMutServer(t, fixedReply(200, m.reply))
				r := runMutationCLI(t, s.srv, 0, nil, args...)
				if r.err != nil {
					t.Fatalf("exit %d: %v", r.exit, r.err)
				}
				reqs := s.requests()
				if len(reqs) != 1 {
					t.Fatalf("%d request(s), want 1", len(reqs))
				}
				if !strings.Contains(reqs[0].body, `"MCP_SERVER_TOKEN_SHARING_PER_USER"`) {
					t.Errorf("body missing per-user token sharing: %s", reqs[0].body)
				}
				for _, n := range c.companions {
					// The omitted secret must be sent empty, which is what
					// makes the preview honest; only the supplied ones are checked.
					if v := c.values[n]; v != "" && !strings.Contains(reqs[0].body, `"`+jsonKeyFor(n)+`":"`+v+`"`) {
						t.Errorf("body missing %s value %q: %s", n, v, reqs[0].body)
					}
				}
			})
		}
	}

	// A custom header name is still required, even per-user and with a coarse mask.
	for _, cmdName := range []string{"register", "update-credentials"} {
		t.Run(cmdName+"/custom-header/name-required", func(t *testing.T) {
			m := authFlagMutations()[cmdName]
			args := append(append([]string{}, m.args...), "--auth=custom-header", "--token-sharing=per-user")
			withDryRun(t)
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, fixedReply(200, m.reply))
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.exit != exitUsage || r.err == nil || !strings.Contains(r.err.Error(), "--header-name") {
				t.Fatalf("exit %d (%v), want 2 naming --header-name", r.exit, r.err)
			}
			if len(s.requests()) != 0 || r.stdout != "" {
				t.Errorf("%d request(s), stdout %q; want none and no preview", len(s.requests()), r.stdout)
			}
		})
	}
}

// TestMcpAuthExplicitMaskScopesRequirements pins the second exception: an
// explicit --update-mask requires only the arm fields it names, because the
// server carries the rest forward from the stored server. A coarse arm path
// names all of them.
func TestMcpAuthExplicitMaskScopesRequirements(t *testing.T) {
	type maskCase struct {
		name    string
		auth    string
		flags   []string
		mask    string
		wantErr string // "" means accepted; otherwise the flag the error names
	}
	cases := []maskCase{
		{"basic password only", "basic-auth", []string{"--basic-auth-password=NEW"}, "basicAuth.password", ""},
		{"basic password only snake", "basic-auth", []string{"--basic-auth-password=NEW"}, "basic_auth.password", ""},
		{"basic username only", "basic-auth", []string{"--basic-auth-username=USER"}, "basicAuth.username", ""},
		{"basic coarse needs both", "basic-auth", []string{"--basic-auth-password=NEW"}, "basicAuth", "--basic-auth-username"},
		{"custom name only", "custom-header", []string{"--header-name=X-Key"}, "customHeader.headerName", ""},
		{"custom value only", "custom-header", []string{"--header-value=v"}, "customHeader.headerValue", ""},
		{"custom coarse needs both", "custom-header", []string{"--header-name=X-Key"}, "customHeader", "--header-value"},
		{"bearer token", "bearer-token", []string{"--bearer-token=tok"}, "bearerToken.token", ""},
		{"bearer masked but missing", "bearer-token", nil, "bearerToken.token", "--bearer-token"},
		{"mask names no arm field", "basic-auth", nil, "url", ""},
	}
	for _, tc := range cases {
		for _, cmdName := range []string{"update-credentials", "test-connection"} {
			t.Run(cmdName+"/"+tc.name, func(t *testing.T) {
				var args []string
				if cmdName == "update-credentials" {
					args = []string{"mcp", "servers", "update-credentials", "cn1", "--app-id=app1", "--type=external"}
				} else {
					// Edit mode: the mask names changed fields on a stored server.
					args = []string{"mcp", "servers", "test-connection", "cn1", "--app-id=app1"}
				}
				args = append(args, "--auth="+tc.auth, "--update-mask="+tc.mask)
				args = append(args, tc.flags...)
				t.Setenv("C1I_URL", mutTestURL)
				s := newMutServer(t, fixedReply(200, `{"reachable":true,"mcpServer":{"connectorId":"cn1"}}`))
				r := runMutationCLI(t, s.srv, 0, nil, args...)
				if tc.wantErr != "" {
					if r.exit != exitUsage || r.err == nil || !strings.Contains(r.err.Error(), tc.wantErr) {
						t.Fatalf("exit %d (%v), want 2 naming %s", r.exit, r.err, tc.wantErr)
					}
					if len(s.requests()) != 0 || r.stdout != "" {
						t.Errorf("%d request(s), stdout %q; want none", len(s.requests()), r.stdout)
					}
					return
				}
				if r.err != nil {
					t.Fatalf("exit %d: %v", r.exit, r.err)
				}
				reqs := s.requests()
				if len(reqs) != 1 {
					t.Fatalf("%d request(s), want 1", len(reqs))
				}
				var body map[string]any
				if err := json.Unmarshal([]byte(reqs[0].body), &body); err != nil {
					t.Fatalf("body is not JSON: %v", err)
				}
				if got, _ := body["updateMask"].(string); got != tc.mask {
					t.Errorf("updateMask = %q, want %q", got, tc.mask)
				}
				for _, f := range tc.flags {
					flag, value, _ := strings.Cut(strings.TrimPrefix(f, "--"), "=")
					key := jsonKeyFor(flag)
					cfg, _ := body["externalConfig"].(map[string]any)
					arm, _ := cfg[jsonArmKeyFor(tc.auth)].(map[string]any)
					if arm[key] != value {
						t.Errorf("%s.%s = %v, want %q (body %s)", jsonArmKeyFor(tc.auth), key, arm[key], value, reqs[0].body)
					}
				}
			})
		}
	}
}

// TestMcpAuthCreateProbeIgnoresMask pins that a mask is only trusted where it
// is sent: test-connection's create mode ignores --update-mask, so it still
// needs the full arm.
func TestMcpAuthCreateProbeIgnoresMask(t *testing.T) {
	withDryRun(t)
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, fixedReply(200, `{"reachable":true}`))
	r := runMutationCLI(t, s.srv, 0, nil,
		"mcp", "servers", "test-connection", "--server-url=https://mcp.example.com/sse",
		"--auth=basic-auth", "--update-mask=basicAuth.password")
	if r.exit != exitUsage || r.err == nil || !strings.Contains(r.err.Error(), "--basic-auth-username") {
		t.Fatalf("exit %d (%v), want 2 naming --basic-auth-username", r.exit, r.err)
	}
	if len(s.requests()) != 0 {
		t.Errorf("%d request(s), want none", len(s.requests()))
	}
}

// TestMcpAuthUnchangedShapes pins that --auth none, --print-config-template
// and the config-file escape hatch are untouched by the companion rule.
func TestMcpAuthUnchangedShapes(t *testing.T) {
	t.Run("auth none", func(t *testing.T) {
		withDryRun(t)
		t.Setenv("C1I_URL", mutTestURL)
		s := newMutServer(t, fixedReply(200, `{}`))
		r := runMutationCLI(t, s.srv, 0, nil,
			"mcp", "servers", "register", "--app-id=app1", "--type=external", "--display-name=S", "--server-url=https://mcp.example.com/sse", "--auth=none")
		if r.err != nil {
			t.Fatalf("exit %d: %v", r.exit, r.err)
		}
	})

	t.Run("print config template", func(t *testing.T) {
		t.Setenv("C1I_URL", mutTestURL)
		s := newMutServer(t, nil)
		r := runMutationCLI(t, s.srv, 0, nil, "mcp", "servers", "register", "--print-config-template", "--auth=basic-auth")
		if r.err != nil {
			t.Fatalf("exit %d: %v", r.exit, r.err)
		}
		if !strings.Contains(r.stdout, `"password": "<password>"`) {
			t.Errorf("template = %s, want the basicAuth placeholder arm", r.stdout)
		}
		if len(s.requests()) != 0 {
			t.Errorf("%d request(s), want none", len(s.requests()))
		}
	})

	t.Run("external config file", func(t *testing.T) {
		withDryRun(t)
		t.Setenv("C1I_URL", mutTestURL)
		s := newMutServer(t, fixedReply(200, `{}`))
		r := runMutationCLI(t, s.srv, 0, nil,
			"mcp", "servers", "register", "--app-id=app1", "--type=external", "--display-name=S",
			"--external-config-file", writeTempJSON(t, "ext.json", `{"url":"https://mcp.example.com/sse","basicAuth":{"username":"u","password":"p"}}`))
		if r.err != nil {
			t.Fatalf("exit %d: %v", r.exit, r.err)
		}
		if len(s.requests()) != 0 {
			t.Errorf("%d request(s) sent on --dry-run, want none", len(s.requests()))
		}
	})
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

// jsonArmKeyFor maps a --auth value to its oneof case name in the body.
func jsonArmKeyFor(auth string) string {
	arm, _ := authFlag.parse(auth)
	return arm
}
