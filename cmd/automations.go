package cmd

import "github.com/spf13/cobra"

var automationsCmd = &cobra.Command{
	Use:   "automations",
	Short: "Manage C1 automations and inspect their executions",
	Long: `List automations, inspect a single automation, and query the recent
execution history. The 'executions list' subcommand is the most common
entry point when debugging "did this automation run?" or "why did it
fail?" questions.`,
}

var automationsExecutionsCmd = &cobra.Command{
	Use:   "executions",
	Short: "Inspect automation executions (the run history)",
}

func init() {
	rootCmd.AddCommand(automationsCmd)
	automationsCmd.AddCommand(automationsExecutionsCmd)
}
