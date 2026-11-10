package cmd

import "github.com/spf13/cobra"

var tasksApproveAction = taskAction{
	verb: "approve",
	step: stepRequired,
}

var tasksApproveCmd = &cobra.Command{
	Use:   "approve <task-id>",
	Short: "Approve an access request task",
	Long: `Approve an access request task.

The approval targets a specific step of the task's policy via --policy-step-id.
If omitted, the task's currently executing step is fetched and used
automatically; a task with no current step is an error (exit 2) telling you to
re-read it with "c1i tasks get <task-id>". Pass --policy-step-id only when you
already know the step.

A step c1i looked up is reported on stderr ("Resolved policy step: <id>"),
not on stdout.

Prints the task as pretty JSON, unwrapped like "tasks get": the id is .id.
Its .state is the task from before the action, not the outcome; re-read with
"c1i tasks get <task-id>" before acting on it. --fields is a usage error on a
mutation; extract with jq -er, which fails on a --dry-run preview.`,
	Args: cobra.ExactArgs(1),
	RunE: tasksApproveAction.runTaskAction,
}

func init() {
	tasksApproveCmd.Flags().String("policy-step-id", "", "Policy step to approve (defaults to the task's current step)")
	tasksApproveCmd.Flags().String("comment", "", "Optional comment")
	markMutating(tasksApproveCmd)
	tasksCmd.AddCommand(tasksApproveCmd)
}
