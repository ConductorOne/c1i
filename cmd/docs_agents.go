package cmd

import (
	_ "embed"
	"strings"

	"github.com/spf13/cobra"
)

//go:embed agents.md
var agentsTemplate string

var docsAgentsCmd = &cobra.Command{
	Use:     "agents",
	Aliases: []string{"skill"},
	Short:   "Output a bootstrap doc for AI agents using c1i",
	Long: `Output an agent-facing doc covering the conventions c1i's --help
text can't: getting the tenant right, output contracts, exit codes,
pagination, and when to prefer a first-class command over "c1i api".

It does not enumerate every command or flag — those live in the cobra
command tree itself ("c1i --help", "c1i <group> --help", "c1i <group>
<command> --help"), which can't drift out of sync with the binary the way a
static doc can.

The output opens with a YAML front-matter block (name/description/version/
required_bins) that some agent harnesses parse automatically; the rest is
plain Markdown.

"docs skill" is kept as an alias to this command for backward compatibility.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		content := strings.ReplaceAll(agentsTemplate, "{{VERSION}}", Version)

		return emitDoc(cmd, "AGENTS.md", content)
	},
}

func init() {
	docsAgentsCmd.Flags().StringP("output", "o", "", "write to file instead of stdout (\"-\" is stdout)")
	allowEmpty(docsAgentsCmd, emptyIsOmitted, "output")
	docsCmd.AddCommand(docsAgentsCmd)
}
