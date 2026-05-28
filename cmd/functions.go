package cmd

import "github.com/spf13/cobra"

var functionsCmd = &cobra.Command{
	Use:   "functions",
	Short: "Manage C1 serverless functions",
	Long: `Inspect and operate on C1 functions — the serverless TypeScript modules
that run inside C1's sandbox and are invoked as automation steps, from
the UI, or via the API.

The 'source' subcommand is the most useful for AI agents and humans
auditing what a function does: it auto-resolves the published commit
and base64-decodes the source files for you.`,
}

func init() {
	rootCmd.AddCommand(functionsCmd)
}
