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
		if f.Name == blankFlag {
			args = append(args, "--"+f.Name, value)
		} else if isRequiredFlag(f) && f.Value.Type() == "string" {
			args = append(args, "--"+f.Name, "x")
		}
	})
	return args
}

// TestBlankIDAndRequiredFlagsExitUsage walks the whole command tree: every
// string flag that is required or id-shaped must exit 2 on a
// blank value with nothing sent. One central rule (validateBlankFlags)
// provides this; a per-command check is never needed.
func TestBlankIDAndRequiredFlagsExitUsage(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	checked := 0
	for _, c := range blankCheckCommands() {
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if f.Value.Type() != "string" || (!isRequiredFlag(f) && !strings.HasSuffix(f.Name, "-id")) {
				return
			}
			for _, v := range blankValues {
				checked++
				name := fmt.Sprintf("%s --%s %q", c.CommandPath(), f.Name, v)
				t.Run(name, func(t *testing.T) {
					srv := newMutServer(t, fixedReply(200, "{}"))
					res := runMutationCLI(t, srv.srv, 0, nil, blankArgs(t, c, f.Name, v)...)
					if res.exit != exitUsage {
						t.Fatalf("exit = %d (err %v), want %d", res.exit, res.err, exitUsage)
					}
					if want := "flag --" + f.Name + " requires a non-empty value"; res.err == nil || !strings.Contains(res.err.Error(), want) {
						t.Errorf("err = %v, want it to contain %q", res.err, want)
					}
					if got := srv.requests(); len(got) != 0 {
						t.Errorf("requests sent: %v", got)
					}
				})
			}
		})
	}
	if checked < 200 {
		t.Errorf("only %d blank cases ran; the tree walk has gone vacuous", checked)
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
