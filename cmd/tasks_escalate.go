package cmd

import "github.com/spf13/cobra"

var tasksEscalateAction = taskAction{
	verb: "escalate",
	step: stepRequired,
}

var tasksEscalateCmd = &cobra.Command{
	Use:   "escalate <task-id>",
	Short: "Escalate a grant task to its entitlement's emergency access policy",
	Long: `Move a grant task onto the emergency-grant policy of the entitlement it
requests, in place of the normal approval flow. Afterwards the task's
.emergencyAccess is true and its current step is that policy's first step.

The server allows it only while the task's .actions lists
TASK_ACTION_TYPE_ESCALATE_TO_EMERGENCY_ACCESS; otherwise (for example when the
entitlement has no emergency-grant policy, or the task was already escalated)
it refuses with "action not permitted" (exit 2).

The step id is required by the server, which rejects a missing one with:
  invalid TaskActionsServiceEscalateToEmergencyAccessRequest.PolicyStepId: value does not match regex pattern "^[a-zA-Z0-9]{27}$"
It defaults to the task's currently executing step, so pass --policy-step-id
only to target a different one. Because that step is fetched, --dry-run
authenticates and issues a read against the tenant before previewing.

A step c1i looked up is reported on stderr ("Resolved policy step: <id>"),
not on stdout.

Prints the task as pretty JSON, unwrapped like "tasks get": the id is .id.
Its .state is the task from before the action, not the outcome; re-read with
"c1i tasks get <task-id>" before acting on it. --fields is a usage error on a
mutation; extract with jq -er, which fails on a --dry-run preview.`,
	Args: cobra.ExactArgs(1),
	RunE: tasksEscalateAction.runTaskAction,
}

func init() {
	tasksEscalateCmd.Flags().String("policy-step-id", "", "Policy step to escalate from (defaults to the task's current step)")
	tasksEscalateCmd.Flags().String("comment", "", "Optional comment")
	allowEmpty(tasksEscalateCmd, emptyIsOmitted, "comment")
	markMutating(tasksEscalateCmd)
	tasksCmd.AddCommand(tasksEscalateCmd)
}
