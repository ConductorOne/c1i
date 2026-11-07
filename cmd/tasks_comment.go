package cmd

import "github.com/spf13/cobra"

var tasksCommentAction = taskAction{
	verb: "comment",
	step: stepUnused,
	// Sent unconditionally: the comment is the payload, so --comment "" must
	// reach the server rather than be omitted as an absent option.
	extraBody: func(cmd *cobra.Command, body map[string]any) error {
		comment, _ := cmd.Flags().GetString("comment")
		body["comment"] = comment
		return nil
	},
}

var tasksCommentCmd = &cobra.Command{
	Use:   "comment <task-id>",
	Short: "Add a comment to a task",
	Long: `Add a comment to a task.

Prints the task as pretty JSON, unwrapped like "tasks get": the id is .id.
Its .state is the task from before the action, not the outcome; re-read with
"c1i tasks get <task-id>" before acting on it. --fields is a usage error on a
mutation; extract with jq -er, which fails on a --dry-run preview.`,
	Args: cobra.ExactArgs(1),
	RunE: tasksCommentAction.runTaskAction,
}

func init() {
	tasksCommentCmd.Flags().String("comment", "", "Comment text")
	markRequired(tasksCommentCmd, "comment")
	markMutating(tasksCommentCmd)
	tasksCmd.AddCommand(tasksCommentCmd)
}
