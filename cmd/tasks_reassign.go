package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var tasksReassignAction = taskAction{
	verb: "reassign",
	// The API does not require policyStepId here (approve does). We require a
	// resolvable step anyway: a reassign with no step is ambiguous, and failing
	// loudly beats sending it.
	step: stepRequired,
	extraBody: func(cmd *cobra.Command, body map[string]any) error {
		// Cobra's required check only proves the flag was set; the accessor is
		// what rejects an empty occurrence that would post a blank approver id.
		toUserIDs, err := repeatableStringFlag(cmd, "to-user-id")
		if err != nil {
			return err
		}
		if len(toUserIDs) == 0 {
			return &usageError{fmt.Errorf("flag --to-user-id requires at least one value")}
		}
		body["newStepUserIds"] = toUserIDs
		return nil
	},
	confirm: func(id, _, stepID string) string {
		return fmt.Sprintf("Reassigned task: task_id=%s policy_step_id=%s\n", id, stepID)
	},
}

var tasksReassignCmd = &cobra.Command{
	Use:   "reassign <task-id>",
	Short: "Reassign a task's approval step to other users",
	Long: `Reassign a task's approval step to one or more other users.

--to-user-id sets the step's approvers to the users named; repeat it to
assign several.

--policy-step-id targets a specific step of the task's policy. If omitted, the
task's currently executing step is fetched and used; if it cannot be determined
the command errors and asks you to pass --policy-step-id explicitly.

The confirmation reports the task id and the policy step acted on, never a
state: the action endpoints echo the task's state from before the action.`,
	Args: cobra.ExactArgs(1),
	RunE: tasksReassignAction.runTaskAction,
}

func init() {
	addRepeatableStringFlag(tasksReassignCmd, "to-user-id", "User ID to reassign the step to (repeatable)")
	tasksReassignCmd.Flags().String("policy-step-id", "", "Policy step to reassign (defaults to the task's current step)")
	tasksReassignCmd.Flags().String("comment", "", "Optional comment")
	markRequired(tasksReassignCmd, "to-user-id")
	tasksCmd.AddCommand(tasksReassignCmd)
}
