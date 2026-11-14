package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

var controlValues = []string{"a\nb", "a\rb", "a\x00b", "a\x1bb", "a\x7fb"}

func TestParseKeyValueFlagRefusesControlChars(t *testing.T) {
	for _, v := range controlValues {
		for _, pair := range []string{"K=" + v, v + "=x"} {
			_, err := parseKeyValueFlag([]string{pair}, "header")
			if err == nil || !strings.Contains(err.Error(), "--header") || !strings.Contains(err.Error(), "control character") {
				t.Errorf("pair %q: err = %v, want a --header control-character error", pair, err)
			}
			if _, ok := err.(*usageError); !ok {
				t.Errorf("pair %q: err type %T, want *usageError", pair, err)
			}
		}
	}
	if _, err := parseKeyValueFlag([]string{"K=a\tb"}, "header"); err != nil {
		t.Errorf("tab in a value is legal HTTP: %v", err)
	}
	if _, err := parseKeyValueFlag([]string{"a\tb=x"}, "header"); err == nil {
		t.Error("tab in a key must be refused")
	}
	if _, err := parseKeyValueFlag([]string{"K=a\nb"}, "config-field", allowValueControls); err != nil {
		t.Errorf("config-field values are JSON, a newline is legitimate: %v", err)
	}
	if _, err := parseKeyValueFlag([]string{"a\nb=x"}, "config-field", allowValueControls); err == nil {
		t.Error("a control character in a key is refused for every caller")
	}
}

func TestAPIHeaderControlCharsExitUsage(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	for _, v := range controlValues {
		srv := newMutServer(t, fixedReply(200, "{}"))
		res := runMutationCLI(t, srv.srv, 0, nil, "api", "--path", "/api/v1/apps", "--header", "X-A="+v)
		if res.exit != exitUsage || res.err == nil || !strings.Contains(res.err.Error(), "--header") {
			t.Errorf("%q: exit %d err %v, want usage error naming --header", v, res.exit, res.err)
		}
		if got := srv.requests(); len(got) != 0 {
			t.Errorf("%q: requests sent: %v", v, got)
		}
	}
}

func TestPathFlagsRefuseControlChars(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	checked := 0
	for _, c := range blankCheckCommands() {
		visitStringFlagsWithGlobals(c, func(f *pflag.Flag) {
			if !isPathFlag(f) {
				return
			}
			for _, v := range controlValues {
				checked++
				srv := newMutServer(t, fixedReply(200, "{}"))
				res := runMutationCLI(t, srv.srv, 0, nil, blankArgs(t, c, f.Name, v)...)
				if res.exit != exitUsage || res.err == nil ||
					!strings.Contains(res.err.Error(), "--"+f.Name) || !strings.Contains(res.err.Error(), "control character") {
					t.Errorf("%s --%s %q: exit %d err %v, want a control-character usage error", c.CommandPath(), f.Name, v, res.exit, res.err)
				}
				if got := srv.requests(); len(got) != 0 {
					t.Errorf("%s --%s %q: requests sent: %v", c.CommandPath(), f.Name, v, got)
				}
			}
		})
	}
	if checked < 100 {
		t.Errorf("only %d cases ran; the tree walk has gone vacuous", checked)
	}
}

func TestPathFlagAllowsStdinAndPlainPaths(t *testing.T) {
	for _, v := range []string{"-", "out.md", "dir/sub dir/é.md"} {
		if hasControlChar(v) {
			t.Errorf("%q must be allowed", v)
		}
	}
}

// pathFlagNotPaths are flags whose name or help looks path-like but which
// take no filesystem path.
var pathFlagNotPaths = map[string]string{
	"c1i api --path": "an API URL path, not a file",
	"c1i access-reviews reports generate --format": "help mentions --body-file; the value is a format name",
	"c1i automations update --update-mask":         "help mentions --body-file; the value is a field mask",
	"c1i policies update --update-mask":            "help mentions --body-file; the value is a field mask",
	"c1i mcp servers register --auth":              "help mentions a config file; the value is an auth mode",
	"c1i mcp servers test-connection --auth":       "help mentions a config file; the value is an auth mode",
	"c1i mcp servers update-credentials --auth":    "help mentions a config file; the value is an auth mode",
}

var pathLikeUsage = regexp.MustCompile(`\bfiles?\b|\bdirector(y|ies)\b|"-"`)

// TestEveryPathLikeFlagIsCovered fails when a flag that reads or writes a file
// is missed by isPathFlag, so a new one cannot slip past the control-character
// rule by being named differently.
func TestEveryPathLikeFlagIsCovered(t *testing.T) {
	for _, c := range blankCheckCommands() {
		visitStringFlagsWithGlobals(c, func(f *pflag.Flag) {
			key := c.CommandPath() + " --" + f.Name
			u := strings.ToLower(f.Usage)
			like := pathLikeUsage.MatchString(u)
			for _, seg := range strings.Split(f.Name, "-") {
				like = like || seg == "file" || seg == "dir" || seg == "out" || seg == "output" || seg == "path"
			}
			if !like || isPathFlag(f) {
				return
			}
			if _, ok := pathFlagNotPaths[key]; !ok {
				t.Errorf("%s looks like a path flag but isPathFlag misses it; annotate it or add it to pathFlagNotPaths", key)
			}
		})
	}
}

func TestHeaderFlagsRefuseControlChars(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	cases := map[string][]string{
		"header-name":  append([]string{"a\tb"}, controlValues...),
		"header-value": controlValues,
		"bearer-token": controlValues,
	}
	checked := 0
	for _, c := range blankCheckCommands() {
		for flag, values := range cases {
			if c.Flags().Lookup(flag) == nil {
				continue
			}
			for _, v := range values {
				checked++
				srv := newMutServer(t, fixedReply(200, "{}"))
				res := runMutationCLI(t, srv.srv, 0, nil, blankArgs(t, c, flag, v)...)
				if res.exit != exitUsage || res.err == nil ||
					!strings.Contains(res.err.Error(), "--"+flag) || !strings.Contains(res.err.Error(), "control character") {
					t.Errorf("%s --%s %q: exit %d err %v, want a control-character usage error", c.CommandPath(), flag, v, res.exit, res.err)
				}
				if got := srv.requests(); len(got) != 0 {
					t.Errorf("%s --%s %q: requests sent: %v", c.CommandPath(), flag, v, got)
				}
			}
		}
	}
	if checked != 3*(6+5+5) {
		t.Errorf("ran %d cases, want 3 commands x 16 values", checked)
	}
	for _, c := range blankCheckCommands() {
		for _, flag := range []string{"header-value", "bearer-token"} {
			if c.Flags().Lookup(flag) == nil {
				continue
			}
			srv := newMutServer(t, fixedReply(200, "{}"))
			res := runMutationCLI(t, srv.srv, 0, nil, blankArgs(t, c, flag, "a\tb")...)
			if res.err != nil && strings.Contains(res.err.Error(), "control character") {
				t.Errorf("%s: tab in --%s refused: %v", c.CommandPath(), flag, res.err)
			}
		}
	}
}

// A header value is often a secret: a refusal must name the flag, the
// character and its offset, never the value.
func TestHeaderControlCharErrorNeverEchoesValue(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	const secret = "sekr3t\r\nX: y"
	run := func(args ...string) mutResult {
		srv := newMutServer(t, fixedReply(200, "{}"))
		res := runMutationCLI(t, srv.srv, 0, nil, args...)
		if got := srv.requests(); len(got) != 0 {
			t.Errorf("%v: requests sent: %v", args, got)
		}
		return res
	}
	var runs [][]string
	runs = append(runs, []string{"api", "--path", "/api/v1/apps", "--header", "Authorization=" + secret})
	for _, flag := range []string{"header-value", "bearer-token"} {
		for _, c := range blankCheckCommands() {
			if c.Flags().Lookup(flag) != nil {
				runs = append(runs, blankArgs(t, c, flag, secret))
			}
		}
	}
	for _, args := range runs {
		for _, format := range []string{"text", "json"} {
			res := run(append([]string{"--error-format", format}, args...)...)
			out := res.stdout + res.stderr
			if res.err != nil {
				out += res.err.Error()
			}
			if res.exit != exitUsage || !regexp.MustCompile(`control character U\+000[AD] at byte 6`).MatchString(out) {
				t.Errorf("%v (%s): exit %d out %q, want a usage error naming the character at byte 6", args, format, res.exit, out)
			}
			for _, leak := range []string{"sekr3t", "X: y"} {
				if strings.Contains(out, leak) {
					t.Errorf("%v (%s): output echoes the value (%q): %q", args, format, leak, out)
				}
			}
		}
	}
	if len(runs) != 7 {
		t.Errorf("ran %d invocations, want api + 3 commands x 2 flags", len(runs))
	}
}

// A malformed key=value occurrence is often a secret missing its "=" (a pasted
// token), so its error must name the flag only, never the input.
func TestMalformedKeyValueErrorNeverEchoesInput(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	const secret = "sekr3tTokenNoEquals"
	for _, args := range [][]string{
		{"api", "--path", "/api/v1/apps", "--header", secret},
		{"api", "--path", "/api/v1/apps", "--query", secret},
		{"api", "--path", "/api/v1/apps", "--header", "=" + secret},
		{"mcp", "servers", "register", "--app-id", "a", "--type", "hosted", "--display-name", "d", "--config-field", secret},
	} {
		for _, format := range []string{"text", "json"} {
			srv := newMutServer(t, fixedReply(200, "{}"))
			res := runMutationCLI(t, srv.srv, 0, nil, append([]string{"--error-format", format}, args...)...)
			out := res.stdout + res.stderr
			if res.err != nil {
				out += res.err.Error()
			}
			if res.exit != exitUsage || !strings.Contains(out, "expected key=value") {
				t.Errorf("%v (%s): exit %d out %q, want the key=value usage error", args, format, res.exit, out)
			}
			if strings.Contains(out, "sekr3t") {
				t.Errorf("%v (%s): output echoes the input: %q", args, format, out)
			}
			if got := srv.requests(); len(got) != 0 {
				t.Errorf("%v: requests sent: %v", args, got)
			}
		}
	}
}

func TestHeaderNameRequiresTokenChars(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	for _, pair := range []string{"Authorization: Bearer sekr3t==", "A B=x", "A(B)=x", "\u00e9=x"} {
		srv := newMutServer(t, fixedReply(200, "{}"))
		res := runMutationCLI(t, srv.srv, 0, nil, "api", "--path", "/api/v1/apps", "--header", pair)
		out := res.stderr
		if res.err != nil {
			out += res.err.Error()
		}
		if res.exit != exitUsage || !strings.Contains(out, "--header") || !strings.Contains(out, "name has invalid character") {
			t.Errorf("%q: exit %d out %q, want a name usage error", pair, res.exit, out)
		}
		if strings.Contains(out, "sekr3t") || strings.Contains(out, "Authorization") || strings.Contains(out, "Bearer") {
			t.Errorf("%q: error echoes the name: %q", pair, out)
		}
		if got := srv.requests(); len(got) != 0 {
			t.Errorf("%q: requests sent: %v", pair, got)
		}
	}
	for _, c := range blankCheckCommands() {
		if c.Flags().Lookup("header-name") == nil {
			continue
		}
		srv := newMutServer(t, fixedReply(200, "{}"))
		res := runMutationCLI(t, srv.srv, 0, nil, blankArgs(t, c, "header-name", "Authorization: sekr3t")...)
		if res.exit != exitUsage || res.err == nil || strings.Contains(res.err.Error(), "sekr3t") || !strings.Contains(res.err.Error(), "--header-name") {
			t.Errorf("%s: exit %d err %v, want a --header-name usage error without the name", c.CommandPath(), res.exit, res.err)
		}
	}
	if _, err := parseKeyValueFlag([]string{"X-Request_Id.1~=v"}, "header"); err != nil {
		t.Errorf("a name of token characters must pass: %v", err)
	}
}

// The same rule applies to auth fields inside a config file, which bypass the flags.
func TestConfigFileHeaderFieldsRefuseControlChars(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	const secret = `sekr3t\r\nX: y` // JSON-escaped control characters
	cmds := map[string][]string{
		"register hosted":   {"mcp", "servers", "register", "--app-id", "a", "--type", "hosted", "--display-name", "d", "--hosted-config-file"},
		"register external": {"mcp", "servers", "register", "--app-id", "a", "--type", "external", "--display-name", "d", "--external-config-file"},
		"creds hosted":      {"mcp", "servers", "update-credentials", "conn-1", "--app-id", "a", "--type", "hosted", "--update-mask", "hostedConfig", "--hosted-config-file"},
		"creds external":    {"mcp", "servers", "update-credentials", "conn-1", "--app-id", "a", "--type", "external", "--update-mask", "externalConfig", "--external-config-file"},
		"test-connection":   {"mcp", "servers", "test-connection", "--external-config-file"},
	}
	arms := map[string]struct{ json, field string }{
		"header value":       {`{"customHeader":{"headerName":"X-K","headerValue":"` + secret + `"}}`, "customHeader.headerValue"},
		"header value snake": {`{"custom_header":{"header_name":"X-K","header_value":"` + secret + `"}}`, "custom_header.header_value"},
		"header name":        {`{"customHeader":{"headerName":"Authorization: ` + secret + `","headerValue":"v"}}`, "customHeader.headerName"},
		"bearer":             {`{"bearerToken":{"token":"` + secret + `"}}`, "bearerToken.token"},
		"bearer snake":       {`{"bearer_token":{"token":"` + secret + `"}}`, "bearer_token.token"},
	}
	ran := 0
	for cname, base := range cmds {
		for aname, arm := range arms {
			for _, dry := range []bool{false, true} {
				body := arm.json
				if strings.Contains(cname, "hosted") {
					body = strings.Replace(body, "{", `{"mcpServerCatalogId":"cat1",`, 1)
				}
				file := filepath.Join(t.TempDir(), "cfg.json")
				if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
				args := append(append([]string{}, base...), file)
				if dry {
					args = append(args, "--dry-run")
				}
				srv := newMutServer(t, fixedReply(200, "{}"))
				res := runMutationCLI(t, srv.srv, 0, nil, args...)
				out := res.stdout + res.stderr
				if res.err != nil {
					out += res.err.Error()
				}
				ran++
				if res.exit != exitUsage || !strings.Contains(out, arm.field) || !strings.Contains(out, "character") {
					t.Errorf("%s/%s dry=%v: exit %d out %q, want a usage error naming %s", cname, aname, dry, res.exit, out, arm.field)
				}
				if strings.Contains(out, "sekr3t") || strings.Contains(out, "X: y") {
					t.Errorf("%s/%s dry=%v: output echoes the value: %q", cname, aname, dry, out)
				}
				if got := srv.requests(); len(got) != 0 {
					t.Errorf("%s/%s dry=%v: requests sent: %v", cname, aname, dry, got)
				}
			}
		}
	}
	if ran != 50 {
		t.Errorf("ran %d cases, want 50", ran)
	}
}
