package cmd

import (
	"regexp"
	"testing"

	"github.com/spf13/cobra"
)

// statesOutput matches a help text that says what the command prints.
var statesOutput = regexp.MustCompile(`(?i)\bprints?\b|\(pretty JSON\)`)

// Every mutating command's --help (its Long, or its Short when it has none,
// as cobra renders it) states its output shape.
func TestMutatingHelpStatesOutputShape(t *testing.T) {
	var visit func(c *cobra.Command)
	visit = func(c *cobra.Command) {
		for _, s := range c.Commands() {
			visit(s)
		}
		help := c.Long
		if help == "" {
			help = c.Short
		}
		if isMutating(c) && !statesOutput.MatchString(help) {
			t.Errorf("%s: --help does not say what it prints", c.CommandPath())
		}
	}
	visit(rootCmd)
}
