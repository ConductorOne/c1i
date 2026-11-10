package cmd

import "github.com/spf13/cobra"

var tasksDenyAction = taskAction{
	verb: "deny",
	// Optional, not required: when the current step cannot be determined the
	// field is omitted rather than blocking the denial.
	step: stepOptional,
}

var tasksDenyCmd = &cobra.Command{
	Use:   "deny <task-id>",
	Short: "Deny an access request task",
	Long: `Deny an access request task.

--policy-step-id targets a specific step of the task's policy. If omitted, the
currently executing step is used when the task has one and left off when it has
none — deny does not require a step, so either way it proceeds. A task that
cannot be fetched is an error (exit 4 for an unknown id), never a silent
denial without a step.

A step c1i looked up is reported on stderr ("Resolved policy step: <id>"),
not on stdout.

Prints the task as pretty JSON, unwrapped like "tasks get": the id is .id.
Its .state is the task from before the action, not the outcome; re-read with
"c1i tasks get <task-id>" before acting on it. --fields is a usage error on a
mutation; extract with jq -er, which fails on a --dry-run preview.`,
	Args: cobra.ExactArgs(1),
	RunE: tasksDenyAction.runTaskAction,
}

func init() {
	tasksDenyCmd.Flags().String("policy-step-id", "", "Policy step to deny (defaults to the task's current step)")
	tasksDenyCmd.Flags().String("comment", "", "Optional comment")
	markMutating(tasksDenyCmd)
	tasksCmd.AddCommand(tasksDenyCmd)
}
