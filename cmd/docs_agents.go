package cmd

import (
	_ "embed"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

//go:embed agents.md
var agentsTemplate string

var docsAgentsCmd = &cobra.Command{
	Use:   "agents",
	Short: "Output a short bootstrap doc for AI agents using c1i",
	Long: `Output a short, agent-facing doc covering the conventions c1i's --help
text can't: output contracts, exit codes, pagination, and when to prefer
first-class commands over "c1i api".

Unlike "docs skill", this does not enumerate commands — use the cobra
command tree ("c1i --help", "c1i <group> --help") or "docs skill" for that.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		content := strings.ReplaceAll(agentsTemplate, "{{VERSION}}", Version)

		output, _ := cmd.Flags().GetString("output")
		if output != "" {
			if err := os.WriteFile(output, []byte(content), 0o644); err != nil {
				return fmt.Errorf("writing agents file: %w", err)
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Wrote AGENTS.md to %s\n", output)
			return nil
		}

		_, _ = fmt.Fprint(cmd.OutOrStdout(), content)
		return nil
	},
}

func init() {
	docsAgentsCmd.Flags().StringP("output", "o", "", "write to file instead of stdout")
	docsCmd.AddCommand(docsAgentsCmd)
}
