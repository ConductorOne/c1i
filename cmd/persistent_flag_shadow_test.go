package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// A subcommand flag named like a root persistent flag wins on that subcommand,
// so the global one becomes unreachable there and silently means something
// else. Three `mcp servers` subcommands shadowed --url this way, leaving no
// flag that could select the tenant (C105).
//
// Persistent registrations count too, and are worse: they shadow the whole
// subtree, not one command. NonInheritedFlags is the set that distinguishes "my
// own flag" (local or persistent) from "a parent's, passed through", which is
// exactly the distinction a shadow turns on.
func TestNoSubcommandShadowsAPersistentFlag(t *testing.T) {
	// Cobra creates `completion` lazily, so the shipped tree is wider than the one
	// a bare walk sees; Run() calls this for the same reason. `help` is created
	// lazily too and is deliberately not forced here: stamping guards onto it
	// breaks `c1i help <command>` (see Run()), and mutating that shared state from
	// a test would corrupt other tests rather than this one. Both are cobra's own
	// and carry none of our flags, so the residual gap cannot hide a shadow; the
	// floor below is what guards against the walk collapsing.
	rootCmd.InitDefaultCompletionCmd()

	var persistent []string
	rootCmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		persistent = append(persistent, f.Name)
	})
	if len(persistent) == 0 {
		t.Fatal("no root persistent flags found; this guard would pass vacuously")
	}

	visited := 0
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if c != rootCmd {
			visited++
			for _, name := range persistent {
				if c.NonInheritedFlags().Lookup(name) != nil {
					t.Errorf("%q declares its own --%s, shadowing the root persistent flag: "+
						"the global --%s is unreachable on that command%s. Rename the local flag.",
						c.CommandPath(), name, name,
						map[bool]string{true: " and everything under it"}[c.PersistentFlags().Lookup(name) != nil])
				}
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)

	// A walk that silently stopped finding commands would read as coverage. A floor
	// rather than an exact count: whether cobra's lazy `help` exists depends on
	// what else ran first, so an exact number would be flaky, not stricter.
	if visited < 100 {
		t.Errorf("walked only %d commands; the tree has well over 100, so this guard is not seeing it", visited)
	}
}
