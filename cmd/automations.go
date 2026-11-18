package cmd

import "github.com/spf13/cobra"

var automationsCmd = &cobra.Command{
	Use:   "automations",
	Short: "Manage C1 automations and inspect their executions",
	Long: `List, inspect, create, update, execute or delete automations; inspect or
terminate execution history; clear a circuit breaker; and cancel queued
executions. "executions list" is the most common entry point when debugging
"did this automation run?" or "why did it fail?" questions.`,
}

var automationsExecutionsCmd = &cobra.Command{
	Use:   "executions",
	Short: "Inspect or terminate automation executions (the run history)",
	Long:  `List or get an automation's executions, or terminate a running execution.`,
}

func init() {
	rootCmd.AddCommand(automationsCmd)
	automationsCmd.AddCommand(automationsExecutionsCmd)
}
