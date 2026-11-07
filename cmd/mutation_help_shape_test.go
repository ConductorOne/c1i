package cmd

import (
	"regexp"
	"testing"

	"github.com/spf13/cobra"
)

// statesOutput matches the house wording: a "(pretty JSON)" tag, or a
// "Prints"/"print" sentence that names the output (JSON, {}, NDJSON, an
// object, a document, the response, or "unwrapped like <get>"). A bare mention
// of "print" (e.g. --print-config-template) or "Prints nothing" does not count.
var statesOutput = regexp.MustCompile(`(?s)\b[Pp]rints?\s[^.]{0,160}?(JSON|\{\}|NDJSON|\bobject\b|\bdocument\b|\bresponse\b|\bunwrapped\b)|\(pretty JSON\)`)

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

func TestStatesOutputRejectsLooseMentions(t *testing.T) {
	for _, bad := range []string{
		"Register a server. Use --print-config-template to see the shape.",
		"Prints nothing.",
		"Does a thing; prints stuff later.",
	} {
		if statesOutput.MatchString(bad) {
			t.Errorf("statesOutput accepted %q", bad)
		}
	}
	for _, good := range []string{
		"Prints the API's response, which is {}; exit 0 is the success signal.",
		"Prints the app as pretty JSON, unwrapped like \"apps get\".",
		"Delete a thing (pretty JSON)",
	} {
		if !statesOutput.MatchString(good) {
			t.Errorf("statesOutput rejected %q", good)
		}
	}
}
