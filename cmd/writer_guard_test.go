package cmd

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// restoreCmdWriters registers a cleanup that unsets every out/err/in writer in
// the tree. Call it in any test that sets one directly: cobra keeps it on the
// shared command, so a later test running that command writes into this
// test's buffer. Helpers that restore their own writers (runCLI) don't need it.
func restoreCmdWriters(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { clearWriters(rootCmd) })
}

func clearWriters(c *cobra.Command) {
	c.SetOut(nil)
	c.SetErr(nil)
	c.SetIn(nil)
	for _, sub := range c.Commands() {
		clearWriters(sub)
	}
}

// leakedWriters names every command in c's tree whose out/err/in writer is set
// explicitly. Cobra falls back to the parent's writer when unset, so a command
// whose resolved writer differs from its parent's (or the process stream, for
// the root) was set by a test.
func leakedWriters(c *cobra.Command) []string {
	var leaks []string
	add := func(kind string) { leaks = append(leaks, c.CommandPath()+" "+kind) }
	if p := c.Parent(); p == nil {
		if c.OutOrStdout() != os.Stdout {
			add("out")
		}
		if c.ErrOrStderr() != os.Stderr {
			add("err")
		}
		if c.InOrStdin() != os.Stdin {
			add("in")
		}
	} else {
		if c.OutOrStdout() != p.OutOrStdout() {
			add("out")
		}
		if c.ErrOrStderr() != p.ErrOrStderr() {
			add("err")
		}
		if c.InOrStdin() != p.InOrStdin() {
			add("in")
		}
	}
	for _, sub := range c.Commands() {
		leaks = append(leaks, leakedWriters(sub)...)
	}
	return leaks
}

// checkWritersAfterRun is TestMain's guard: a writer still set once every test
// has finished was left by a test that did not restore it. Returns the exit
// code to use.
func checkWritersAfterRun(code int) int {
	if leaks := leakedWriters(rootCmd); len(leaks) > 0 {
		fmt.Fprintf(os.Stderr, "FAIL: tests left command writers set (call restoreCmdWriters(t) in the test that sets them): %s\n", strings.Join(leaks, ", "))
		if code == 0 {
			return 1
		}
	}
	return code
}

func TestLeakedWritersDetectsAnExplicitWriter(t *testing.T) {
	restoreCmdWriters(t)
	if got := leakedWriters(rootCmd); len(got) != 0 {
		t.Fatalf("writers already set before the test: %v", got)
	}
	appsCmd.SetOut(&bytes.Buffer{})
	got := leakedWriters(rootCmd)
	if len(got) != 1 || !strings.HasSuffix(got[0], "apps out") {
		t.Fatalf("leakedWriters = %v, want only the apps out writer", got)
	}
}

func TestRestoreCmdWritersClearsEveryWriter(t *testing.T) {
	t.Run("set", func(t *testing.T) {
		restoreCmdWriters(t)
		rootCmd.SetErr(&bytes.Buffer{})
		appsCmd.SetIn(strings.NewReader(""))
		appsCmd.SetOut(&bytes.Buffer{})
	})
	if got := leakedWriters(rootCmd); len(got) != 0 {
		t.Fatalf("writers survived the subtest: %v", got)
	}
}
