package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var tasksSkipStepAction = taskAction{
	verb:      "skip-step",
	step:      stepRequired,
	stepCheck: skipStepApprovalOnly,
}

// skipStepApprovalOnly refuses the current step when it is not an approval.
// The API accepts a provision step too, but skipping one abandons provisioning
// and can strand or close the task, so that needs an explicit --policy-step-id.
func skipStepApprovalOnly(taskID string, data []byte) error {
	_, kind, err := parseCurrentPolicyStep(data)
	if err != nil {
		return err
	}
	if kind == "approval" {
		return nil
	}
	return &usageError{fmt.Errorf("task %s is not on an approval step; skip-step refuses it (re-read with \"c1i tasks get %s\"; pass --policy-step-id explicitly only if you intend to skip that step)", taskID, taskID)}
}

var tasksSkipStepCmd = &cobra.Command{
	Use:   "skip-step <task-id>",
	Short: "Skip a task's current policy step",
	Long: `Skip the task's current approval step, advancing the policy without a decision.

A current provision step is refused: skipping it abandons provisioning and can
leave the task stranded or close it. To skip one on purpose, name it with
--policy-step-id; that explicit form is sent as given.

The step id is required by the server, which rejects a missing one with:
  invalid TaskActionsServiceSkipStepRequest.PolicyStepId: value does not match regex pattern "^[a-zA-Z0-9]{27}$"
It defaults to the task's currently executing step, so pass --policy-step-id
only to target a different one. Because that step is fetched, --dry-run
authenticates and issues a read against the tenant before previewing. A step
id captured before another action is stale, and the server answers
"this action is no longer available".

A step c1i looked up is reported on stderr ("Resolved policy step: <id>"),
not on stdout.

Prints the task as pretty JSON, unwrapped like "tasks get": the id is .id.
Its .state is the task from before the action, not the outcome; re-read with
"c1i tasks get <task-id>" before acting on it. --fields is a usage error on a
mutation; extract with jq -er, which fails on a --dry-run preview.`,
	Args: cobra.ExactArgs(1),
	RunE: tasksSkipStepAction.runTaskAction,
}

func init() {
	tasksSkipStepCmd.Flags().String("comment", "", "Optional comment")
	allowEmpty(tasksSkipStepCmd, emptyIsOmitted, "comment")
	tasksSkipStepCmd.Flags().String("policy-step-id", "", "Policy step to skip (defaults to the task's current step)")
	markMutating(tasksSkipStepCmd)
	tasksCmd.AddCommand(tasksSkipStepCmd)
}
