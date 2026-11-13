package cmd

import "github.com/spf13/cobra"

var tasksCloseAction = taskAction{
	verb: "close",
	step: stepUnused,
}

var tasksCloseCmd = &cobra.Command{
	Use:   "close <task-id>",
	Short: "Close a task without approving or denying it",
	Long: `Close a task without approving or denying it.

Closing cancels the task and records no approval decision; use approve/deny to
record an outcome.

Prints the task as pretty JSON, unwrapped like "tasks get": the id is .id.
Its .state is the task from before the action, not the outcome; re-read with
"c1i tasks get <task-id>" before acting on it. --fields is a usage error on a
mutation; extract with jq -er, which fails on a --dry-run preview.`,
	Args: cobra.ExactArgs(1),
	RunE: tasksCloseAction.runTaskAction,
}

func init() {
	tasksCloseCmd.Flags().String("comment", "", "Optional comment")
	allowEmpty(tasksCloseCmd, emptyIsOmitted, "comment")
	markMutating(tasksCloseCmd)
	tasksCmd.AddCommand(tasksCloseCmd)
}
