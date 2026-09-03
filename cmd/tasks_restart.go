package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var tasksRestartAction = taskAction{
	verb: "restart",
	step: stepOptional,
	confirm: func(id, _, stepID string) string {
		// A closed task has no current step.
		if stepID == "" {
			return fmt.Sprintf("Restarted task: task_id=%s\n", id)
		}
		return fmt.Sprintf("Restarted task: task_id=%s policy_step_id=%s\n", id, stepID)
	},
}

var tasksRestartCmd = &cobra.Command{
	Use:   "restart <task-id>",
	Short: "Restart a task's approval step",
	Long: `Restart a task's approval step, sending it back for a fresh decision.

On an open task this rotates the current policy step and records the restart in
the task's policy history. It does NOT reopen a closed task: the API offers
restart on some closed tasks, but the call leaves the state CLOSED and only
appends history, so use it to re-run a live approval, not to undo a close.

Whether restart is available at all depends on the task; the server refuses
with "action not permitted" otherwise. Check the task's own action list:
  c1i api --path /api/v1/tasks/<task-id> --fields actions

restart, reset and skip-step each rotate the current policy step, so a
--policy-step-id captured before one of them goes stale and the server answers:
  this action is no longer available: the request has advanced to a new approval step
Omit the flag to act on whatever step is current.

Because the step is fetched, --dry-run authenticates and issues a read against
the tenant before printing its preview.

The confirmation reports the task id and the step acted on, never a state: the
action endpoints echo the task's state from before the action.`,
	Args: cobra.ExactArgs(1),
	RunE: tasksRestartAction.runTaskAction,
}

func init() {
	tasksRestartCmd.Flags().String("comment", "", "Optional comment")
	tasksRestartCmd.Flags().String("policy-step-id", "", "Policy step to restart (defaults to the task's current step)")
	tasksCmd.AddCommand(tasksRestartCmd)
}
