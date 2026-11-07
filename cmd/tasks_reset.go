package cmd

import "github.com/spf13/cobra"

var tasksResetAction = taskAction{
	verb: "reset",
	step: stepUnused,
}

var tasksResetCmd = &cobra.Command{
	Use:   "reset <task-id>",
	Short: "Hard-reset a task to the start of its policy",
	Long: `Hard-reset a task, returning it to the beginning of its policy.

Unlike "restart", which re-runs the current step, this discards the task's
approval progress and starts the policy over. The endpoint is
/action/reset and takes no policy step.

Prints the task as pretty JSON, unwrapped like "tasks get": the id is .id.
Its .state is the task from before the action, not the outcome; re-read with
"c1i tasks get <task-id>" before acting on it. --fields is a usage error on a
mutation; extract with jq -er, which fails on a --dry-run preview.`,
	Args: cobra.ExactArgs(1),
	RunE: tasksResetAction.runTaskAction,
}

func init() {
	tasksResetCmd.Flags().String("comment", "", "Optional comment")
	markMutating(tasksResetCmd)
	tasksCmd.AddCommand(tasksResetCmd)
}
