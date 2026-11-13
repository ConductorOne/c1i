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
