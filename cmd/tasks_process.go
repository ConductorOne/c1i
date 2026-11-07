package cmd

import "github.com/spf13/cobra"

var tasksProcessAction = taskAction{
	verb: "process",
	step: stepUnused,
}

var tasksProcessCmd = &cobra.Command{
	Use:   "process <task-id>",
	Short: "Process a task now rather than waiting for the next cycle",
	Long: `Ask C1 to process a task immediately instead of on its normal schedule.

Intended for a task that looks stuck: it asks C1 to re-run the policy
evaluation without changing the task's approval state. The request body is empty — this action
takes neither a comment nor a policy step.

On a healthy task nothing observable changes: state, current policy step and
history are all identical afterwards. The stalled case was not reproduced, so
treat any effect there as unverified.

Prints the task as pretty JSON, unwrapped like "tasks get": the id is .id.
Its .state is the task from before the action, not the outcome; re-read with
"c1i tasks get <task-id>" before acting on it. --fields is a usage error on a
mutation; extract with jq -er, which fails on a --dry-run preview.`,
	Args: cobra.ExactArgs(1),
	RunE: tasksProcessAction.runTaskAction,
}

func init() {
	// No --comment: the ProcessNow request body carries only expandMask.
	markMutating(tasksProcessCmd)
	tasksCmd.AddCommand(tasksProcessCmd)
}
