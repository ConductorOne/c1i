package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var tasksApproveAction = taskAction{
	verb: "approve",
	step: stepRequired,
	confirm: func(id, state, _ string) string {
		return fmt.Sprintf("Approved task: task_id=%s state=%s\n", id, state)
	},
}

var tasksApproveCmd = &cobra.Command{
	Use:   "approve <task-id>",
	Short: "Approve an access request task",
	Long: `Approve an access request task.

The approval targets a specific step of the task's policy via --policy-step-id.
If omitted, the task's currently executing step is fetched and used
automatically; if it cannot be determined the command errors and asks you to
pass --policy-step-id explicitly (approve requires a step).`,
	Args: cobra.ExactArgs(1),
	RunE: tasksApproveAction.runTaskAction,
}

func init() {
	tasksApproveCmd.Flags().String("policy-step-id", "", "Policy step to approve (defaults to the task's current step)")
	tasksApproveCmd.Flags().String("comment", "", "Optional comment")
	tasksCmd.AddCommand(tasksApproveCmd)
}
