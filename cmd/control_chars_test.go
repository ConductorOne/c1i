package cmd

import (
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
