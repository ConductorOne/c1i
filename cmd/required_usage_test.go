package cmd

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// requiredClaim finds "(required" or "[required" in usage text.
var requiredClaim = regexp.MustCompile(`(?i)([(\[])required`)

// conditionalRequired is what may follow "(required" in a mode rule.
var conditionalRequired = regexp.MustCompile(`(?i)^\s+(with|unless|when|if)\b`)

// handWrittenRequiredClaim returns the first "required" claim in usage that is
// neither the literal lowercase "(required)" nor a conditional like
// "(required with ...)".
func handWrittenRequiredClaim(usage string) (string, bool) {
	for _, m := range requiredClaim.FindAllStringSubmatchIndex(usage, -1) {
		rest := usage[m[1]:]
		if usage[m[0]:m[1]] == "(required" && (strings.HasPrefix(rest, ")") || conditionalRequired.MatchString(rest)) {
			continue
		}
		return usage[m[0]:min(len(usage), m[1]+12)], true
	}
	return "", false
}

// A hand-written "(required)" carries no annotation, so the tree tests below
// cannot hold it to enforcement.
func TestRequiredUsageMatchesAnnotation(t *testing.T) {
	checked := 0
	walkCommandTree(func(c *cobra.Command) {
		c.Flags().VisitAll(func(f *pflag.Flag) {
			says := strings.Contains(strings.ToLower(f.Usage), "(required)")
			has := f.Annotations[requiredAnnotation] != nil
			if says || has {
				checked++
			}
			if says && !has {
				t.Errorf("%s --%s says \"(required)\" without requiredAnnotation; register it with markRequired or annotateRequired", c.CommandPath(), f.Name)
			}
			if has && !says {
				t.Errorf("%s --%s carries requiredAnnotation but its usage omits \"(required)\"", c.CommandPath(), f.Name)
			}
			if claim, bad := handWrittenRequiredClaim(f.Usage); bad && !says {
				t.Errorf("%s --%s usage claims %q (%q); annotate it, or word a mode rule as \"(required with|unless|when|if ...)\"", c.CommandPath(), f.Name, claim, f.Usage)
			}
		})
	})
	if checked < 20 {
		t.Errorf("only %d required flags seen; the tree walk has gone vacuous", checked)
	}
}

func TestHandWrittenRequiredClaim(t *testing.T) {
	for usage, bad := range map[string]bool{
		"x (required)": false, "x (required with --a)": false, "x (Required unless --a)": true,
		"x (required; file)": true, "x (REQUIRED, y)": true, "x [required]": true, "x (required)  (required;": true,
		"https required.": false, "x (Required)": true, "x (REQUIRED)": true, "x (requireds)": true,
	} {
		if _, got := handWrittenRequiredClaim(usage); got != bad {
			t.Errorf("%q: flagged = %v, want %v", usage, got, bad)
		}
	}
}

var (
	missingFlagsErr = regexp.MustCompile(`required flag\(s\) ((?:"[^"]+"(?:, )?)+) not set`)
	flagIsRequired  = regexp.MustCompile(`^--([a-z0-9-]+) is required(\s*\(|\s+and\b|$)`)
	flagNeedsValue  = regexp.MustCompile(`^flag --([a-z0-9-]+) requires at least one value`)
)

// missingFlags returns the flags a missing-flag usage error names.
func missingFlags(err error) []string {
	msg := err.Error()
	if m := missingFlagsErr.FindStringSubmatch(msg); m != nil {
		var out []string
		for _, q := range strings.Split(m[1], ", ") {
			out = append(out, strings.Trim(q, `"`))
		}
		return out
	}
	for _, re := range []*regexp.Regexp{flagIsRequired, flagNeedsValue} {
		if m := re.FindStringSubmatch(msg); m != nil {
			return []string{m[1]}
		}
	}
	return nil
}

// requiredFlagSkips are subtrees the required-flag tests leave alone.
var requiredFlagSkips = map[string]string{
	"c1i auth":        "reads and writes local credentials and runs the device flow",
	"c1i docs":        "calls fixed public hosts",
	"c1i mcp gateway": "mints a token and builds its own gateway client; it has no required flags",
	"c1i upgrade":     "calls the distribution host and replaces the binary",
}

// conditionalRequiredFlags demand a flag in the command's default mode only;
// their usage must say so ("(required unless ...)") instead of "(required)".
var conditionalRequiredFlags = map[string]string{
	"c1i automations create --display-name":              "unless --body-file",
	"c1i policies create --display-name":                 "unless --body-file",
	"c1i policies create --policy-type":                  "unless --body-file",
	"c1i connectors create --catalog-id":                 "unless --body-file",
	"c1i access-reviews templates create --display-name": "unless --body-file",
	"c1i access-reviews templates create --policy-id":    "unless --body-file",
	"c1i access-reviews templates create --owner-id":     "unless --body-file",
}

func skippedRequiredFlagCommand(c *cobra.Command) bool {
	for skip := range requiredFlagSkips {
		if p := c.CommandPath(); p == skip || strings.HasPrefix(p, skip+" ") {
			return true
		}
	}
	return false
}

// stubEveryClient points every REST client constructor at srv and restores them
// on cleanup. With requiredFlagSkips (docs, upgrade, auth and the gateway build
// their own clients), no command under test can reach the network.
func stubEveryClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	build := func(*cobra.Command, string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client(), client.WithMaxRetries(0)), nil
	}
	origClient, origList, origPolicies, origSP, origAPI := newClient, newListClient, newPoliciesClient, newServicePrincipalsClient, newAPIClient
	newClient, newListClient, newPoliciesClient, newServicePrincipalsClient = build, build, build, build
	newAPIClient = func(cmd *cobra.Command, base string) (apiRequester, error) { return build(cmd, base) }
	t.Cleanup(func() {
		newClient, newListClient, newPoliciesClient, newServicePrincipalsClient, newAPIClient = origClient, origList, origPolicies, origSP, origAPI
	})
}

// Every flag a command's own code demands with nothing else given must say so
// in --help. Each command runs with its positionals only, and each flag its
// missing-flag usage error names is then supplied and the run repeated; every
// flag so named must be annotated.
func TestUnconditionallyRequiredFlagsAreAnnotated(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	srv := newMutServer(t, fixedReply(200, "{}"))
	stubEveryClient(t, srv.srv)
	hits, ran := 0, 0
	outcomes := map[string]int{}
	conditionalHit := map[string]bool{}
	for _, c := range blankCheckCommands() {
		if skippedRequiredFlagCommand(c) {
			continue
		}
		args := requiredInvocation(t, c, "x", func(string) bool { return true }, "string", "stringArray")
		for i := range 8 {
			ran++
			res := runCLI(t, nil, nil, args...)
			missing := missingFlags(orEmpty(res.err))
			if i == 0 {
				switch {
				case res.err == nil:
					outcomes["exit 0"]++
				case len(missing) > 0:
					outcomes["missing flag"]++
				case res.exit == exitUsage:
					outcomes["other exit 2"]++
				default:
					outcomes[fmt.Sprintf("exit %d", res.exit)]++
				}
			}
			if res.err == nil {
				break
			}
			if len(missing) == 0 {
				break
			}
			hits += len(missing)
			for _, n := range missing {
				f := c.Flags().Lookup(n)
				if f == nil {
					t.Errorf("%s: error names unknown flag --%s", c.CommandPath(), n)
					continue
				}
				key := c.CommandPath() + " --" + n
				if rule, ok := conditionalRequiredFlags[key]; ok {
					conditionalHit[key] = true
					if isRequiredFlag(f) || !strings.Contains(f.Usage, "(required "+rule+")") {
						t.Errorf("%s is conditionally required; its usage must say \"(required %s)\", not %q", key, rule, f.Usage)
					}
				} else if !isRequiredFlag(f) {
					t.Errorf("%s --%s is demanded at run time (%q) but not annotated required", c.CommandPath(), n, res.err)
				}
				args = append(args, "--"+n, "x")
			}
		}
	}
	for key := range conditionalRequiredFlags {
		if !conditionalHit[key] {
			t.Errorf("conditionalRequiredFlags entry %q was never demanded at run time; remove or fix it", key)
		}
	}
	t.Logf("%d runs, %d missing-flag hits; first-run outcomes %v", ran, hits, outcomes)
	if hits < 120 {
		t.Errorf("only %d missing-flag errors seen; the tree walk has lost reach", hits)
	}
}

// The converse: omitting any one annotated-required flag, with the rest
// present, must be refused as a missing flag.
func TestAnnotatedRequiredFlagsAreEnforced(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	srv := newMutServer(t, fixedReply(200, "{}"))
	stubEveryClient(t, srv.srv)
	checked := 0
	for _, c := range blankCheckCommands() {
		if skippedRequiredFlagCommand(c) {
			continue
		}
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if f.Annotations[requiredAnnotation] == nil {
				return
			}
			checked++
			res := runCLI(t, nil, nil, requiredInvocation(t, c, "x", func(n string) bool { return n == f.Name }, "string", "stringArray")...)
			if res.exit != exitUsage || !slices.Contains(missingFlags(orEmpty(res.err)), f.Name) {
				t.Errorf("%s without --%s: exit = %d, err = %v, want a missing-flag usage error naming it", c.CommandPath(), f.Name, res.exit, res.err)
			}
		})
	}
	if checked < 120 {
		t.Errorf("only %d flags checked; the tree walk has gone vacuous", checked)
	}
}

// orEmpty lets a nil error reach missingFlags.
func orEmpty(err error) error {
	if err == nil {
		return errors.New("")
	}
	return err
}

func TestBlankUpdateMaskExitsUsage(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	body := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(body, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range [][]string{
		{"access-reviews", "update", "abc"},
		{"mcp", "classifiers", "update", "abc"},
	} {
		for _, v := range blankValues {
			srv := newMutServer(t, fixedReply(200, "{}"))
			args := append(append([]string(nil), path...), "--body-file", body, "--update-mask", v)
			res := runMutationCLI(t, srv.srv, 0, nil, args...)
			if res.exit != exitUsage || res.err == nil || !strings.Contains(res.err.Error(), "flag --update-mask requires a non-empty value") {
				t.Errorf("%v mask %q: exit = %d, err = %v, want the central blank-flag usage error", path, v, res.exit, res.err)
			}
			if got := srv.requests(); len(got) != 0 {
				t.Errorf("%v mask %q: requests sent: %v", path, v, got)
			}
		}
	}
}
