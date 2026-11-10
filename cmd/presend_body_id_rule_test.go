package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// A filter id is covered by the same *-id rule even though it only reaches a
// search body.
func TestDotSegmentFilterIDFlagRefused(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	srv := newMutServer(t, fixedReply(200, `{"list":[],"nextPageToken":""}`))
	res := runListCLI(t, srv.srv, "grants", "list", "--user-id", ".")
	if res.exit != exitUsage {
		t.Fatalf("exit = %d, err = %v; want %d", res.exit, res.err, exitUsage)
	}
	if res.err == nil || !strings.Contains(res.err.Error(), "is not an id") {
		t.Errorf("err = %v, want the dot-segment flag refusal", res.err)
	}
	if got := srv.requests(); len(got) != 0 {
		t.Errorf("requests sent: %v", got)
	}
}

// markBodyIDArgs appends: a command marked twice keeps both indexes.
func TestMarkBodyIDArgsAppends(t *testing.T) {
	c := &cobra.Command{Use: "x"}
	markBodyIDArgs(c, 1)
	markBodyIDArgs(c, 2)
	if got, want := c.Annotations[bodyIDArgAnnotation], "1,2"; got != want {
		t.Errorf("annotation = %q, want %q", got, want)
	}
}

// Every *-id flag is a plain string or a repeatable string array, so the root
// pre-run's dot-segment rule always sees its value: a future StringSlice (or an
// int) would otherwise escape the validator silently.
func TestIDFlagsAreStringOrStringArray(t *testing.T) {
	checked := 0
	walkCommandTree(func(c *cobra.Command) {
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if !strings.HasSuffix(f.Name, "-id") {
				return
			}
			checked++
			switch f.Value.Type() {
			case "string", "stringArray":
			default:
				t.Errorf("%s: --%s is %q; an *-id flag must be a string or a repeatable string so the dot-segment pre-send rule can read it", c.CommandPath(), f.Name, f.Value.Type())
			}
		})
	})
	if checked == 0 {
		t.Fatal("walked the command tree and found no *-id flag at all — this guard is not looking at what it thinks it is")
	}
}
