package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var tasksSkipStepAction = taskAction{
	verb: "skip-step",
	step: stepRequired,
	confirm: func(id, _, stepID string) string {
		return fmt.Sprintf("Skipped policy step: task_id=%s policy_step_id=%s\n", id, stepID)
	},
}

var tasksSkipStepCmd = &cobra.Command{
	Use:   "skip-step <task-id>",
	Short: "Skip a task's current policy step",
	Long: `Skip a task's current approval step, advancing the policy without a decision.

The step id is required by the server, which rejects a missing one with:
  invalid TaskActionsServiceSkipStepRequest.PolicyStepId: value does not match regex pattern "^[a-zA-Z0-9]{27}$"
It defaults to the task's currently executing step, so pass --policy-step-id
only to target a different one. Because that step is fetched, --dry-run
authenticates and issues a read against the tenant before previewing. A step id captured before another action is
stale, and the server answers "this action is no longer available".

The confirmation reports the task id and the step skipped, never a state: the
action endpoints echo the task's state from before the action.`,
	Args: cobra.ExactArgs(1),
	RunE: tasksSkipStepAction.runTaskAction,
}

func init() {
	tasksSkipStepCmd.Flags().String("comment", "", "Optional comment")
	tasksSkipStepCmd.Flags().String("policy-step-id", "", "Policy step to skip (defaults to the task's current step)")
	tasksCmd.AddCommand(tasksSkipStepCmd)
}
