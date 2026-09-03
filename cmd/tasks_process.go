package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var tasksProcessAction = taskAction{
	verb: "process",
	step: stepUnused,
	confirm: func(id, _, _ string) string {
		return fmt.Sprintf("Queued task for processing: task_id=%s\n", id)
	},
}

var tasksProcessCmd = &cobra.Command{
	Use:   "process <task-id>",
	Short: "Process a task now rather than waiting for the next cycle",
	Long: `Ask C1 to process a task immediately instead of on its normal schedule.

Useful when a task looks stuck: it re-runs the policy evaluation without
changing the task's approval state. The request body is empty — this action
takes neither a comment nor a policy step.

On a healthy task nothing observable changes: state, current policy step and
history are all identical afterwards. Expect a result only where processing had
genuinely stalled.

The confirmation reports only the task id: the action endpoints echo the task's
state from before the action, and processing is asynchronous, so re-read the
task with "c1i requests get <task-id>" to see the result.`,
	Args: cobra.ExactArgs(1),
	RunE: tasksProcessAction.runTaskAction,
}

func init() {
	// No --comment: the ProcessNow request body carries only expandMask.
	tasksCmd.AddCommand(tasksProcessCmd)
}
