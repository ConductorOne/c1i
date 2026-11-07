package cmd

import "github.com/spf13/cobra"

var tasksSkipStepAction = taskAction{
	verb: "skip-step",
	step: stepRequired,
}

var tasksSkipStepCmd = &cobra.Command{
	Use:   "skip-step <task-id>",
	Short: "Skip a task's current policy step",
	Long: `Skip a task's current approval step, advancing the policy without a decision.

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
	tasksSkipStepCmd.Flags().String("policy-step-id", "", "Policy step to skip (defaults to the task's current step)")
	markMutating(tasksSkipStepCmd)
	tasksCmd.AddCommand(tasksSkipStepCmd)
}
