package cmd

import (
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// A scripted `--wait=$W --wait-timeout X` must keep working when W is false:
// the companions are ignored, as before the stray-flag rule.
func TestWaitFlagValuesAcceptsExplicitWaitFalse(t *testing.T) {
	c := &cobra.Command{Use: "x"}
	addWaitFlags(c, "something until it happens", 4*time.Minute)
	mustSet(t, c.Flags(), "wait", "false")
	mustSet(t, c.Flags(), "wait-timeout", "0s")
	wait, _, err := waitFlagValues(c)
	if err != nil || wait {
		t.Errorf("got (wait=%v, err=%v), want (false, nil)", wait, err)
	}
}
