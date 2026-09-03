package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

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
	confirm: func(id, _, _ string) string {
		return fmt.Sprintf("Comment added: task_id=%s\n", id)
	},
}

var tasksCommentCmd = &cobra.Command{
	Use:   "comment <task-id>",
	Short: "Add a comment to a task",
	Args:  cobra.ExactArgs(1),
	RunE:  tasksCommentAction.runTaskAction,
}

func init() {
	tasksCommentCmd.Flags().String("comment", "", "Comment text")
	markRequired(tasksCommentCmd, "comment")
	tasksCmd.AddCommand(tasksCommentCmd)
}
