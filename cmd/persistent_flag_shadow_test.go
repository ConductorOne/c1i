package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// A subcommand-local flag with the same name as a root persistent flag wins on
// that subcommand, so the global one becomes unreachable there and silently
// means something else. Three `mcp servers` subcommands shadowed --url this
// way, leaving no flag that could select the tenant (C105).
func TestNoSubcommandShadowsAPersistentFlag(t *testing.T) {
	var persistent []string
	rootCmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		persistent = append(persistent, f.Name)
	})
	if len(persistent) == 0 {
		t.Fatal("no root persistent flags found; this guard would pass vacuously")
	}

	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if c != rootCmd {
			for _, name := range persistent {
				if f := c.Flags().Lookup(name); f != nil && c.LocalFlags().Lookup(name) != nil {
					if c.PersistentFlags().Lookup(name) == nil && !isInherited(c, name) {
						t.Errorf("%q registers a local --%s, shadowing the root persistent flag: "+
							"the global --%s is unreachable on that command. Rename the local flag.",
							c.CommandPath(), name, name)
					}
				}
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
}

// isInherited reports whether name reaches c only via a parent's persistent
// flag set, which is the normal case and not a shadow.
func isInherited(c *cobra.Command, name string) bool {
	return c.InheritedFlags().Lookup(name) != nil && c.NonInheritedFlags().Lookup(name) == nil
}
