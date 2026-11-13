package cmd

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// waitSampleValue is a valid value for a wait-* flag of the given type.
func waitSampleValue(t *testing.T, f *pflag.Flag) string {
	t.Helper()
	switch f.Value.Type() {
	case "duration":
		return "5s"
	case "int":
		return "5"
	}
	t.Fatalf("--%s has type %s; teach waitSampleValue", f.Name, f.Value.Type())
	return ""
}

// Every command with --wait refuses a --wait-* flag given without it, before
// any request is sent.
func TestWaitCompanionFlagsRequireWait(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	checked := 0
	for _, c := range blankCheckCommands() {
		if c.Flags().Lookup("wait") == nil {
			continue
		}
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if f.Name == "wait" || !strings.HasPrefix(f.Name, "wait-") {
				return
			}
			checked++
			srv := newMutServer(t, fixedReply(200, "{}"))
			args := blankArgs(t, c, "", "x")
			c.Flags().VisitAll(func(r *pflag.Flag) {
				if isRequiredFlag(r) && r.Value.Type() == "stringArray" {
					args = append(args, "--"+r.Name, "x")
				}
			})
			if c.Flags().Lookup("user-id") != nil && !strings.Contains(strings.Join(args, " "), "--user-id") {
				args = append(args, "--user-id", "x") // grants list needs a filter
			}
			args = append(args, "--"+f.Name, waitSampleValue(t, f), "--dry-run")
			res := runMutationCLI(t, srv.srv, 0, nil, args...)
			want := "--" + f.Name + " requires --wait"
			if res.exit != exitUsage || res.err == nil || !strings.Contains(res.err.Error(), want) {
				t.Errorf("%s: exit = %d, err = %v, want exit 2 naming %q", strings.Join(args, " "), res.exit, res.err, want)
			}
			if got := srv.requests(); len(got) != 0 {
				t.Errorf("%s: requests sent: %v", strings.Join(args, " "), got)
			}
		})
	}
	if checked < 3 {
		t.Errorf("only %d wait-* flags seen; the tree walk has gone vacuous", checked)
	}
}

func TestWaitFlagValuesRequiresWaitForCompanions(t *testing.T) {
	newCmd := func() *cobra.Command {
		c := &cobra.Command{Use: "x"}
		addWaitFlags(c, "something until it happens", 4*time.Minute)
		return c
	}
	c := newCmd()
	mustSet(t, c.Flags(), "wait-timeout", "5s")
	_, _, err := waitFlagValues(c)
	var ue *usageError
	if !errors.As(err, &ue) || !strings.Contains(err.Error(), "--wait-timeout requires --wait") {
		t.Fatalf("err = %v, want usageError naming --wait-timeout requires --wait", err)
	}
	c = newCmd()
	mustSet(t, c.Flags(), "wait", "true")
	mustSet(t, c.Flags(), "wait-timeout", "5s")
	if _, _, err := waitFlagValues(c); err != nil {
		t.Errorf("with --wait: %v", err)
	}
}
