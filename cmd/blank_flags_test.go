package cmd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// "" is the unset-shell-variable case; --flag "" sets the flag, unlike omitting it.
var blankValues = []string{"", "  ", "\u200b"}

// blankCheckCommands returns every runnable command in the real tree except
// cobra's own help/completion machinery.
func blankCheckCommands() []*cobra.Command {
	var out []*cobra.Command
	walkCommandTree(func(c *cobra.Command) {
		if !c.Runnable() || c == rootCmd {
			return
		}
		for p := c; p != nil; p = p.Parent() {
			if n := p.Name(); n == "help" || n == "completion" || strings.HasPrefix(n, "__") {
				return
			}
		}
		out = append(out, c)
	})
	return out
}

// isRequiredStringFlag reports whether f is documented required, via cobra's
// own annotation (markRequired) or ours (annotateRequired).
func isRequiredFlag(f *pflag.Flag) bool {
	return f.Annotations[cobra.BashCompOneRequiredFlag] != nil || f.Annotations[requiredAnnotation] != nil
}

// blankArgs builds a valid-looking invocation of c with every required flag
// and positional filled with a placeholder, except flag blankFlag, which gets
// value. blankFlag "" blanks the first positional instead.
func blankArgs(t *testing.T, c *cobra.Command, blankFlag, value string) []string {
	t.Helper()
	spec, err := parseUsePositionals(c.Use)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Fields(strings.TrimPrefix(c.CommandPath(), rootCmd.Name()+" "))
	for i := 0; i < spec.required; i++ {
		if blankFlag == "" && i == 0 {
			args = append(args, value)
		} else {
			args = append(args, "x")
		}
	}
	c.Flags().VisitAll(func(f *pflag.Flag) {
		if isRequiredFlag(f) && f.Value.Type() == "string" && f.Name != blankFlag {
			args = append(args, "--"+f.Name, "x")
		}
	})
	if blankFlag != "" {
		args = append(args, "--"+blankFlag, value)
	}
	return args
}

// TestBlankStringFlagsExitUsage walks the whole command tree, inherited
// global flags included: every string flag must exit 2 on a blank value with
// nothing sent; an allowEmpty flag still must on whitespace or invisible-only.
// One central rule (validateBlankFlags) provides this.
func TestBlankStringFlagsExitUsage(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	checked := 0
	for _, c := range blankCheckCommands() {
		visitStringFlagsWithGlobals(c, func(f *pflag.Flag) {
			values := blankValues
			if f.Annotations[emptyAllowedAnnotation] != nil {
				values = blankValues[1:]
			}
			for _, v := range values {
				checked++
				name := fmt.Sprintf("%s --%s %q", c.CommandPath(), f.Name, v)
				t.Run(name, func(t *testing.T) {
					srv := newMutServer(t, fixedReply(200, "{}"))
					res := runMutationCLI(t, srv.srv, 0, nil, blankArgs(t, c, f.Name, v)...)
					if res.exit != exitUsage {
						t.Fatalf("exit = %d (err %v), want %d", res.exit, res.err, exitUsage)
					}
					if res.err == nil || !strings.Contains(res.err.Error(), "--"+f.Name) || !strings.Contains(res.err.Error(), blankFlagMessage(f.Name)) {
						t.Errorf("err = %v, want the blank-value error for --%s", res.err, f.Name)
					}
					if got := srv.requests(); len(got) != 0 {
						t.Errorf("requests sent: %v", got)
					}
				})
			}
		})
	}
	if checked < 1500 {
		t.Errorf("only %d blank cases ran; the tree walk has gone vacuous", checked)
	}
}

// runEAfterBlankCheck runs c's RunE the way production does after rootCmd's
// pre-run: blank flags are refused first. For tests that call RunE directly.
func runEAfterBlankCheck(c *cobra.Command, args []string) error {
	if err := validateBlankFlags(c); err != nil {
		return err
	}
	return c.RunE(c, args)
}

// visitStringFlagsWithGlobals visits c's own string flags and rootCmd's
// persistent ones, which cobra merges into c only at execution.
func visitStringFlagsWithGlobals(c *cobra.Command, fn func(*pflag.Flag)) {
	seen := map[string]bool{}
	visit := func(f *pflag.Flag) {
		if f.Value.Type() == "string" && !seen[f.Name] {
			seen[f.Name] = true
			fn(f)
		}
	}
	c.Flags().VisitAll(visit)
	rootCmd.PersistentFlags().VisitAll(visit)
}

// blankFlagMessage is the part of the blank-value error every flag shares;
// --page-token words its own.
func blankFlagMessage(name string) string {
	if name == "page-token" {
		return "is blank"
	}
	return "requires a non-empty value"
}

// An opt-out must say why, and a flag whose help promises that an empty value
// clears something must be opted out, or the rule would refuse what the help
// offers. A filter never is: an empty one would silently widen a list.
func TestBlankAllowedFlagsAreJustified(t *testing.T) {
	allowed := 0
	for _, c := range blankCheckCommands() {
		visitStringFlagsWithGlobals(c, func(f *pflag.Flag) {
			reason := f.Annotations[emptyAllowedAnnotation]
			if reason != nil {
				allowed++
				if len(reason) != 1 || !hasVisibleRune(reason[0]) {
					t.Errorf("%s --%s: allowEmpty needs a reason, got %q", c.CommandPath(), f.Name, reason)
				}
				if strings.Contains(strings.ToLower(f.Usage), "filter") {
					t.Errorf("%s --%s: a filter must not allowEmpty", c.CommandPath(), f.Name)
				}
				return
			}
			if u := strings.ToLower(f.Usage); strings.Contains(u, "empty value") && strings.Contains(u, "clear") {
				t.Errorf("%s --%s: help mentions an empty value but the flag is not opted out with allowEmpty", c.CommandPath(), f.Name)
			}
		})
	}
	if allowed == 0 {
		t.Error("no flag is opted out; the description-clearing update flags should be")
	}
}

// An opted-out flag sends its empty value instead of being refused.
func TestAllowedBlankFlagIsSent(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	srv := newMutServer(t, fixedReply(200, `{"app":{"id":"abc"}}`))
	res := runMutationCLI(t, srv.srv, 0, nil, "apps", "update", "abc", "--description", "")
	if res.exit != 0 {
		t.Fatalf("exit = %d (err %v), want 0", res.exit, res.err)
	}
	if got := srv.requests(); len(got) == 0 {
		t.Fatal("no request sent")
	}
}

// TestBlankPositionalsExitUsage covers the positional twin: ExactArgs accepts
// " ", and an id built from it addresses the collection.
func TestBlankPositionalsExitUsage(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	checked := 0
	for _, c := range blankCheckCommands() {
		if spec, _ := parseUsePositionals(c.Use); spec.required == 0 {
			continue
		}
		for _, v := range blankValues {
			checked++
			t.Run(fmt.Sprintf("%s %q", c.CommandPath(), v), func(t *testing.T) {
				srv := newMutServer(t, fixedReply(200, "{}"))
				res := runMutationCLI(t, srv.srv, 0, nil, blankArgs(t, c, "", v)...)
				if res.exit != exitUsage || res.err == nil || !strings.Contains(res.err.Error(), "is blank") {
					t.Fatalf("exit = %d, err = %v, want a blank-argument usage error", res.exit, res.err)
				}
				if got := srv.requests(); len(got) != 0 {
					t.Errorf("requests sent: %v", got)
				}
			})
		}
	}
	if checked < 100 {
		t.Errorf("only %d positional cases ran; the tree walk has gone vacuous", checked)
	}
	for _, args := range [][]string{{"users", "get", " "}, {"apps", "delete", " "}} {
		res := runMutationCLI(t, nil, 0, nil, args...)
		if res.exit != exitUsage {
			t.Errorf("%v: exit = %d, want %d", args, res.exit, exitUsage)
		}
	}
}

// Whitespace-only values reach the shared checkers' one predicate too.
func TestBlankPredicateIsOneEverywhere(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	cmd.Flags().String("a", "", "")
	addRepeatableStringFlag(cmd, "r", "")
	_ = cmd.Flags().Set("a", "\u200b")
	_ = cmd.Flags().Set("r", "\u200b")
	if err := requireNonEmpty(cmd, "a"); err == nil {
		t.Error("requireNonEmpty accepted an invisible-only value")
	}
	if _, err := repeatableStringFlag(cmd, "r"); err == nil {
		t.Error("repeatableStringFlag accepted an invisible-only value")
	}
}

// The policy-id flags are blank-checked centrally like any *-id flag; only the
// update commands, which have --unbind-policy, add the unbind hint.
func TestBlankPolicyIDFlagsExitUsageWithHint(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	cases := []struct {
		args  []string
		kinds []policyKind
		hint  bool
	}{
		{[]string{"apps", "create", "--display-name", "x"}, appPolicyKinds, false},
		{[]string{"apps", "update", "abc"}, appPolicyKinds, true},
		{[]string{"entitlements", "create", "--app-id", "a", "--display-name", "x"}, entitlementPolicyKinds, false},
		{[]string{"entitlements", "update", "abc", "--app-id", "a"}, entitlementPolicyKinds, true},
	}
	for _, tc := range cases {
		for _, k := range tc.kinds {
			for _, v := range blankValues {
				t.Run(fmt.Sprintf("%s --%s %q", strings.Join(tc.args[:2], " "), k.flag, v), func(t *testing.T) {
					srv := newMutServer(t, fixedReply(200, "{}"))
					args := append(append([]string(nil), tc.args...), "--"+k.flag, v)
					res := runMutationCLI(t, srv.srv, 0, nil, args...)
					want := "flag --" + k.flag + " requires a non-empty value"
					if res.exit != exitUsage || res.err == nil || !strings.Contains(res.err.Error(), want) {
						t.Fatalf("exit = %d, err = %v, want exit 2 containing %q", res.exit, res.err, want)
					}
					if got := strings.Contains(res.err.Error(), "--unbind-policy "+k.kind); got != tc.hint {
						t.Errorf("unbind hint present = %v, want %v: %v", got, tc.hint, res.err)
					}
					if got := srv.requests(); len(got) != 0 {
						t.Errorf("requests sent: %v", got)
					}
				})
			}
		}
	}
}
