package cmd

import "github.com/spf13/cobra"

var tasksGetCmd = &cobra.Command{
	Use:   "get <task-id>",
	Short: "Get a single task by ID (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	Long: `Get a single task by ID: the full task view, including its current policy
step and outcome. It reads the same endpoint as "c1i requests get" (access
requests are tasks); this is the task-side lens for an ID from "c1i tasks list".`,
	RunE: runTaskGet,
}

func init() {
	tasksCmd.AddCommand(tasksGetCmd)
}
