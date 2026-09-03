package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var tasksCloseAction = taskAction{
	verb: "close",
	step: stepUnused,
	confirm: func(id, _, _ string) string {
		return fmt.Sprintf("Closed task: task_id=%s\n", id)
	},
}

var tasksCloseCmd = &cobra.Command{
	Use:   "close <task-id>",
	Short: "Close a task without approving or denying it",
	Long: `Close a task without approving or denying it.

Closing cancels the task and records no approval decision; use approve/deny to
record an outcome. The confirmation reports only the task id — the action
endpoints echo the task's pre-close state, so printing it would be wrong.`,
	Args: cobra.ExactArgs(1),
	RunE: tasksCloseAction.runTaskAction,
}

func init() {
	tasksCloseCmd.Flags().String("comment", "", "Optional comment")
	tasksCmd.AddCommand(tasksCloseCmd)
}
