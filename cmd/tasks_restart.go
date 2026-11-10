package cmd

import "github.com/spf13/cobra"

var tasksRestartAction = taskAction{
	verb: "restart",
	step: stepOptional,
}

var tasksRestartCmd = &cobra.Command{
	Use:   "restart <task-id>",
	Short: "Restart a task's approval step",
	Long: `Restart a task's approval step, sending it back for a fresh decision.

On an open task this rotates the current policy step and records the restart in
the task's policy history. It does NOT reopen a closed task: the server refuses
one with "task is closed" (exit 2), so use it to re-run a live approval, not to
undo a close ("tasks reset" is the one that reopens).

Whether restart is available at all depends on the task; the server refuses
with "action not permitted" otherwise. Check the task's own action list:
  c1i api --path /api/v1/tasks/<task-id> --fields actions

restart, reset and skip-step each rotate the current policy step, so a
--policy-step-id captured before one of them goes stale and the server answers:
  this action is no longer available: the request has advanced to a new approval step
Omit the flag to act on whatever step is current.

Because the step is fetched, --dry-run authenticates and issues a read against
the tenant before printing its preview.

A step c1i looked up is reported on stderr ("Resolved policy step: <id>"),
not on stdout.

Prints the task as pretty JSON, unwrapped like "tasks get": the id is .id.
Its .state is the task from before the action, not the outcome; re-read with
"c1i tasks get <task-id>" before acting on it. --fields is a usage error on a
mutation; extract with jq -er, which fails on a --dry-run preview.`,
	Args: cobra.ExactArgs(1),
	RunE: tasksRestartAction.runTaskAction,
}

func init() {
	tasksRestartCmd.Flags().String("comment", "", "Optional comment")
	tasksRestartCmd.Flags().String("policy-step-id", "", "Policy step to restart (defaults to the task's current step)")
	markMutating(tasksRestartCmd)
	tasksCmd.AddCommand(tasksRestartCmd)
}
