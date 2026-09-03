package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var tasksResetAction = taskAction{
	verb: "reset",
	step: stepUnused,
	confirm: func(id, _, _ string) string {
		return fmt.Sprintf("Reset task: task_id=%s\n", id)
	},
}

var tasksResetCmd = &cobra.Command{
	Use:   "reset <task-id>",
	Short: "Hard-reset a task to the start of its policy",
	Long: `Hard-reset a task, returning it to the beginning of its policy.

Unlike "restart", which re-runs the current step, this discards the task's
approval progress and starts the policy over. The endpoint is
/action/reset and takes no policy step.

The confirmation reports only the task id: the action endpoints echo the task's
state from before the action.`,
	Args: cobra.ExactArgs(1),
	RunE: tasksResetAction.runTaskAction,
}

func init() {
	tasksResetCmd.Flags().String("comment", "", "Optional comment")
	tasksCmd.AddCommand(tasksResetCmd)
}
