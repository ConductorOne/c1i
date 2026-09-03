package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var tasksDenyAction = taskAction{
	verb: "deny",
	// Optional, not required: when the current step cannot be determined the
	// field is omitted rather than blocking the denial.
	step: stepOptional,
	confirm: func(id, state, _ string) string {
		return fmt.Sprintf("Denied task: task_id=%s state=%s\n", id, state)
	},
}

var tasksDenyCmd = &cobra.Command{
	Use:   "deny <task-id>",
	Short: "Deny an access request task",
	Long: `Deny an access request task.

--policy-step-id targets a specific step of the task's policy. If omitted, the
currently executing step is used when it can be derived, and simply left off
otherwise — deny does not require a step, so it proceeds either way.`,
	Args: cobra.ExactArgs(1),
	RunE: tasksDenyAction.runTaskAction,
}

func init() {
	tasksDenyCmd.Flags().String("policy-step-id", "", "Policy step to deny (defaults to the task's current step)")
	tasksDenyCmd.Flags().String("comment", "", "Optional comment")
	tasksCmd.AddCommand(tasksDenyCmd)
}
