package cmd

import "github.com/spf13/cobra"

var tasksRetryProvisioningAction = taskAction{
	verb: "retry-provisioning",
	step: stepRequired,
}

var tasksRetryProvisioningCmd = &cobra.Command{
	Use:   "retry-provisioning <task-id>",
	Short: "Retry a task's failed connector provisioning",
	Long: `Retry the connector provisioning of a task whose provision step failed.
The API describes it as resetting the failed connector actions and re-driving
the connector while keeping the approvals already collected.

The server allows it only while the task's .actions lists
TASK_ACTION_TYPE_RETRY_PROVISIONING; on a task at an approval step it refuses
with "action not permitted" (exit 2).

The step id is required by the server, which rejects a missing one with:
  invalid TaskActionsServiceRetryProvisioningRequest.PolicyStepId: value does not match regex pattern "^[a-zA-Z0-9]{27}$"
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
	RunE: tasksRetryProvisioningAction.runTaskAction,
}

func init() {
	tasksRetryProvisioningCmd.Flags().String("policy-step-id", "", "Provision step to retry (defaults to the task's current step)")
	tasksRetryProvisioningCmd.Flags().String("comment", "", "Optional comment")
	allowEmpty(tasksRetryProvisioningCmd, emptyIsOmitted, "comment")
	markMutating(tasksRetryProvisioningCmd)
	tasksCmd.AddCommand(tasksRetryProvisioningCmd)
}
