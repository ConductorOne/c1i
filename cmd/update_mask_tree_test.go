package cmd

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// updateMaskCases drives every command that has an --update-mask flag; a new
// one fails TestUpdateMaskRuleIsOneRuleAcrossTree until it is listed here.
// body is true where --body-file carries the object (the mask is then
// required with it).
var updateMaskCases = map[string]struct {
	args []string
	body bool
}{
	"policies update":                   {[]string{"policies", "update", "p-1"}, true},
	"automations update":                {[]string{"automations", "update", "a-1"}, true},
	"access-reviews update":             {[]string{"access-reviews", "update", "c-1"}, true},
	"mcp classifiers update":            {[]string{"mcp", "classifiers", "update", "cl-1"}, true},
	"mcp classifiers policy update":     {[]string{"mcp", "classifiers", "policy", "update"}, true},
	"mcp classifiers tool-gates update": {[]string{"mcp", "classifiers", "tool-gates", "update", "tg-1"}, true},
	"mcp servers test-connection":       {[]string{"mcp", "servers", "test-connection", "cn-1", "--app-id", "a1", "--server-url", "https://mcp.example.invalid"}, false},
	"mcp servers update-credentials":    {[]string{"mcp", "servers", "update-credentials", "cn-1", "--app-id", "a1", "--type", "external", "--server-url", "https://mcp.example.invalid"}, false},
}

func commandsWithUpdateMask(c *cobra.Command, out map[string]bool) {
	if c.Flags().Lookup("update-mask") != nil && c.Runnable() {
		out[strings.TrimPrefix(c.CommandPath(), rootCmd.Name()+" ")] = true
	}
	for _, sub := range c.Commands() {
		commandsWithUpdateMask(sub, out)
	}
}

func TestUpdateMaskRuleIsOneRuleAcrossTree(t *testing.T) {
	found := map[string]bool{}
	commandsWithUpdateMask(rootCmd, found)
	var names []string
	for n := range found {
		names = append(names, n)
		if _, ok := updateMaskCases[n]; !ok {
			t.Errorf("%q has --update-mask but no case in updateMaskCases", n)
		}
	}
	for n := range updateMaskCases {
		if !found[n] {
			t.Errorf("updateMaskCases lists %q, which has no --update-mask flag", n)
		}
	}
	sort.Strings(names)

	for _, name := range names {
		tc := updateMaskCases[name]
		for _, dry := range []bool{false, true} {
			run := func(t *testing.T, mask string, omitBody bool) (mutResult, *mutServer) {
				t.Setenv("C1I_URL", mutTestURL)
				clearViperOverrides(t, "dry_run")
				s := newMutServer(t, fixedReply(200, `{"reachable":true}`))
				args := append([]string{}, tc.args...)
				if tc.body && !omitBody {
					args = append(args, "--body-file", writeTempJSON(t, "body.json", `{"displayName":"n"}`))
				}
				args = append(args, "--update-mask", mask)
				if dry {
					args = append(args, "--dry-run")
				}
				return runMutationCLI(t, s.srv, 0, nil, args...), s
			}
			suffix := "/send"
			if dry {
				suffix = "/dry-run"
			}
			for _, bad := range []string{",", " ", "a,,b", ",a", "a,", "a..b", ".a", "a.", "basicAuth..password"} {
				t.Run(name+suffix+"/bad mask "+bad, func(t *testing.T) {
					r, s := run(t, bad, false)
					if r.exit != exitUsage || len(s.requests()) != 0 {
						t.Errorf("exit %d (%v), %d request(s); want exit 2, none", r.exit, r.err, len(s.requests()))
					}
				})
			}
			if tc.body {
				t.Run(name+suffix+"/mask without body-file", func(t *testing.T) {
					r, s := run(t, "a", true)
					if r.exit != exitUsage || len(s.requests()) != 0 {
						t.Errorf("exit %d (%v), %d request(s); want exit 2, none", r.exit, r.err, len(s.requests()))
					}
				})
			}
			t.Run(name+suffix+"/padded mask is trimmed", func(t *testing.T) {
				// The credential commands send --server-url, which the mask must name.
				padded, want := " a , b ", "a,b"
				if !tc.body {
					padded, want = " url , b ", "url,b"
				}
				r, s := run(t, padded, false)
				if r.exit != 0 {
					t.Fatalf("exit %d (%v), want 0", r.exit, r.err)
				}
				var got string
				if dry {
					var preview struct {
						Requests []struct{ Body map[string]any } `json:"requests"`
					}
					if err := json.Unmarshal([]byte(r.stdout), &preview); err != nil {
						t.Fatal(err)
					}
					for _, q := range preview.Requests {
						if m, ok := q.Body["updateMask"].(string); ok {
							got = m
						}
					}
				} else {
					for _, q := range s.requests() {
						var b map[string]any
						if json.Unmarshal([]byte(q.body), &b) == nil {
							if m, ok := b["updateMask"].(string); ok {
								got = m
							}
						}
					}
				}
				if got != want {
					t.Errorf("updateMask sent as %q, want %q (stdout %q)", got, want, r.stdout)
				}
			})
		}
	}
}

// A malformed mask is refused before the "nothing to update" check and in
// test-connection's create mode, where the mask is otherwise not sent.
func TestUpdateMaskValidatedBeforeModeChecks(t *testing.T) {
	for name, args := range map[string][]string{
		"update-credentials, no config": {"mcp", "servers", "update-credentials", "cn-1", "--app-id", "a1", "--type", "external", "--update-mask", ","},
		"test-connection, create mode":  {"mcp", "servers", "test-connection", "--server-url", "https://mcp.example.invalid", "--update-mask", "a,,b"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, fixedReply(200, `{"reachable":true}`))
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.exit != exitUsage || !strings.Contains(r.err.Error(), "is empty") || len(s.requests()) != 0 {
				t.Errorf("exit %d (%v), %d request(s); want exit 2 naming the empty path, none", r.exit, r.err, len(s.requests()))
			}
		})
	}
}

// The tree test leaves no viper override behind: C1I_DRY_RUN must still win
// afterwards (a leaked Set("dry_run", false) once made a preview really send).
func TestUpdateMaskTreeTestLeavesDryRunEnvEffective(t *testing.T) {
	t.Run("tree", func(t *testing.T) { TestUpdateMaskRuleIsOneRuleAcrossTree(t) })
	t.Setenv("C1I_URL", mutTestURL)
	t.Setenv("C1I_DRY_RUN", "true")
	s := newMutServer(t, fixedReply(200, `{}`))
	r := runMutationCLI(t, s.srv, 0, nil, "automations", "update", "a-1", "--display-name", "x")
	if r.exit != 0 || len(s.requests()) != 0 {
		t.Errorf("exit %d (%v), %d request(s); want a preview with none sent", r.exit, r.err, len(s.requests()))
	}
}

// The error names the position of the bad path or segment.
func TestUpdateMaskErrorNamesPosition(t *testing.T) {
	for mask, want := range map[string]string{
		"a,,b":                `path 2 is empty (paths are comma-separated, e.g. a,b)`,
		"a,b..c":              `path 2, segment 2 is empty`,
		"basicAuth..password": `path 1, segment 2 is empty`,
	} {
		_, err := normalizeUpdateMask(mask)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("mask %q: err %v, want it to contain %q", mask, err, want)
		}
	}
}

// test-connection sends a mask only in edit mode, so create mode refuses one
// rather than dropping it; "" is "omitted" in both modes.
func TestTestConnectionMaskModes(t *testing.T) {
	create := []string{"mcp", "servers", "test-connection", "--server-url", "https://mcp.example.invalid"}
	edit := []string{"mcp", "servers", "test-connection", "cn-1", "--app-id", "a1"}
	for _, tc := range []struct {
		name     string
		args     []string
		wantExit int
	}{
		{"create, mask", append(append([]string{}, create...), "--update-mask", "url"), exitUsage},
		{"create, empty mask", append(append([]string{}, create...), "--update-mask", ""), 0},
		{"edit, empty mask", append(append([]string{}, edit...), "--update-mask", ""), 0},
		{"edit, mask", append(append([]string{}, edit...), "--update-mask", "url"), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			clearViperOverrides(t, "dry_run")
			s := newMutServer(t, fixedReply(200, `{"reachable":true}`))
			r := runMutationCLI(t, s.srv, 0, nil, tc.args...)
			if r.exit != tc.wantExit {
				t.Fatalf("exit %d (%v), want %d", r.exit, r.err, tc.wantExit)
			}
			if tc.wantExit != 0 {
				if !strings.Contains(r.err.Error(), "--update-mask applies only in edit mode (<connector-id> --app-id)") || len(s.requests()) != 0 {
					t.Errorf("err %v, %d request(s)", r.err, len(s.requests()))
				}
				return
			}
			for _, q := range s.requests() {
				if strings.Contains(q.body, `"updateMask"`) != (strings.Contains(tc.name, "edit, mask")) {
					t.Errorf("body %s: updateMask presence wrong for %q", q.body, tc.name)
				}
			}
		})
	}
}

// An explicit mask must name every top-level config key the command sends;
// otherwise the server silently does not apply it.
func TestExplicitMaskMustCoverSentConfig(t *testing.T) {
	cred := []string{"mcp", "servers", "update-credentials", "cn-1", "--app-id", "a1", "--type", "external"}
	probe := []string{"mcp", "servers", "test-connection", "cn-1", "--app-id", "a1"}
	for _, tc := range []struct {
		name    string
		base    []string
		extra   []string
		wantErr string // "" means accepted
	}{
		{"credentials: url not named", cred, []string{"--server-url", "https://x.invalid", "--update-mask", "a,b"}, "does not name url"},
		{"credentials: url and transport, one named", cred, []string{"--server-url", "https://x.invalid", "--transport", "sse", "--update-mask", "url"}, "does not name transportType"},
		{"credentials: all named", cred, []string{"--server-url", "https://x.invalid", "--transport", "sse", "--update-mask", "url,transportType"}, ""},
		{"credentials: nested path covers its arm", cred, []string{"--auth", "basic-auth", "--basic-auth-password", "p", "--update-mask", "basicAuth.password"}, ""},
		{"credentials: snake path covers its arm", cred, []string{"--auth", "basic-auth", "--basic-auth-password", "p", "--update-mask", "basic_auth.password"}, ""},
		{"credentials: mask with no config", cred, []string{"--update-mask", "url"}, ""},
		{"probe: url not named", probe, []string{"--server-url", "https://x.invalid", "--update-mask", "a,b"}, "does not name url"},
		{"probe: url named", probe, []string{"--server-url", "https://x.invalid", "--update-mask", "url"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			clearViperOverrides(t, "dry_run")
			s := newMutServer(t, fixedReply(200, `{"reachable":true}`))
			r := runMutationCLI(t, s.srv, 0, nil, append(append([]string{}, tc.base...), tc.extra...)...)
			if tc.wantErr == "" {
				if r.exit != 0 {
					t.Fatalf("exit %d (%v), want 0", r.exit, r.err)
				}
				return
			}
			if r.exit != exitUsage || !strings.Contains(r.err.Error(), tc.wantErr) || len(s.requests()) != 0 {
				t.Errorf("exit %d (%v), %d request(s); want exit 2 containing %q, none sent", r.exit, r.err, len(s.requests()), tc.wantErr)
			}
		})
	}
}

// A mask derived from a config file's keys is validated like an explicit one.
func TestDerivedMaskKeysAreValidated(t *testing.T) {
	for _, tc := range []struct {
		name, typ, flag, body, want string
	}{
		{"external empty key", "external", "--external-config-file", `{"":"x","url":"https://x.invalid"}`, "config key 1 is empty"},
		{"external dotted key", "external", "--external-config-file", `{"a..b":"x","url":"https://x.invalid"}`, `config key "a..b" has an empty dot-segment`},
		{"hosted empty key", "hosted", "--hosted-config-file", `{"":"x","mcpServerCatalogId":"c"}`, "config key 1 is empty"},
		{"hosted dotted key", "hosted", "--hosted-config-file", `{".a":"x","mcpServerCatalogId":"c"}`, `config key ".a" has an empty dot-segment`},
	} {
		for _, dry := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/send", true: "/dry-run"}[dry], func(t *testing.T) {
				t.Setenv("C1I_URL", mutTestURL)
				clearViperOverrides(t, "dry_run")
				s := newMutServer(t, fixedReply(200, `{}`))
				args := []string{"mcp", "servers", "update-credentials", "cn-1", "--app-id", "a1", "--type", tc.typ, tc.flag, writeTempJSON(t, "cfg.json", tc.body)}
				if dry {
					args = append(args, "--dry-run")
				}
				r := runMutationCLI(t, s.srv, 0, nil, args...)
				if r.exit != exitUsage || !strings.Contains(r.err.Error(), tc.want) || len(s.requests()) != 0 {
					t.Errorf("exit %d (%v), %d request(s); want exit 2 containing %q, none sent", r.exit, r.err, len(s.requests()), tc.want)
				}
			})
		}
	}
}

// A mask spelled with other casing still scopes the companion-value check
// (same comparison as the coverage rule), so an empty credential is refused.
func TestMaskCasingDoesNotSkipCompanionCheck(t *testing.T) {
	cred := []string{"mcp", "servers", "update-credentials", "cn-1", "--app-id", "a1", "--type", "external"}
	probe := []string{"mcp", "servers", "test-connection", "cn-1", "--app-id", "a1"}
	for name, base := range map[string][]string{"update-credentials": cred, "test-connection": probe} {
		for _, dry := range []bool{false, true} {
			for _, tc := range []struct {
				auth, mask, wantErr string
			}{
				{"bearer-token", "BearerToken", "--bearer-token"},
				{"bearer-token", "BEARERTOKEN.TOKEN", "--bearer-token"},
				{"basic-auth", "basicauth", "--basic-auth-username"},
				{"basic-auth", "BasicAuth.Password", "--basic-auth-password"},
			} {
				t.Run(name+"/"+tc.mask+map[bool]string{false: "/send", true: "/dry-run"}[dry], func(t *testing.T) {
					t.Setenv("C1I_URL", mutTestURL)
					clearViperOverrides(t, "dry_run")
					s := newMutServer(t, fixedReply(200, `{"reachable":true}`))
					args := append(append([]string{}, base...), "--auth", tc.auth, "--update-mask", tc.mask)
					if dry {
						args = append(args, "--dry-run")
					}
					r := runMutationCLI(t, s.srv, 0, nil, args...)
					if r.exit != exitUsage || r.err == nil || !strings.Contains(r.err.Error(), tc.wantErr) || len(s.requests()) != 0 {
						t.Errorf("exit %d (%v), %d request(s); want exit 2 naming %s, none sent", r.exit, r.err, len(s.requests()), tc.wantErr)
					}
				})
			}
		}
	}
}

// Only whole paths are trimmed; a space inside a path goes to the server as given.
func TestNormalizeUpdateMaskTrimsWholePathsOnly(t *testing.T) {
	got, err := normalizeUpdateMask(" a . b , c ")
	if err != nil || got != "a . b,c" {
		t.Errorf("got %q, %v; want %q", got, err, "a . b,c")
	}
	if _, err := normalizeUpdateMask("a. .b"); err == nil {
		t.Error("a blank segment must be refused")
	}
}
