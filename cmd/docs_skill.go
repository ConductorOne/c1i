package cmd

import (
	_ "embed"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

//go:embed skill.md
var skillTemplate string

var docsSkillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Output SKILL.md for AI agents",
	Long: `Output a self-contained SKILL.md that teaches AI agents how to use c1i.

The skill file covers all commands, output formats, API discovery workflows,
and common endpoints. It follows the openclaw frontmatter format.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		content := strings.ReplaceAll(skillTemplate, "{{VERSION}}", Version)

		output, _ := cmd.Flags().GetString("output")
		if output != "" {
			if err := os.WriteFile(output, []byte(content), 0o644); err != nil {
				return fmt.Errorf("writing skill file: %w", err)
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Wrote SKILL.md to %s\n", output)
			return nil
		}

		_, _ = fmt.Fprint(cmd.OutOrStdout(), content)
		return nil
	},
}

func init() {
	docsSkillCmd.Flags().StringP("output", "o", "", "write to file instead of stdout")
	docsCmd.AddCommand(docsSkillCmd)
}
