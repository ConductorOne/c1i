package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var tasksCommentCmd = &cobra.Command{
	Use:   "comment",
	Short: "Add a comment to a task",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := client.New(cmd.Context(), baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		taskID, _ := cmd.Flags().GetString("task-id")
		comment, _ := cmd.Flags().GetString("comment")

		body := map[string]any{
			"comment": comment,
		}

		path := client.Path("/api/v1/tasks/%s/action/comment", taskID)
		data, err := c.Post(cmd.Context(), path, body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		id, _, err := parseTaskActionResponse(data)
		if err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Comment added: task_id=%s\n", id)
		return nil
	},
}

func init() {
	tasksCommentCmd.Flags().String("task-id", "", "Task ID to comment on")
	tasksCommentCmd.Flags().String("comment", "", "Comment text")
	markRequired(tasksCommentCmd, "task-id")
	markRequired(tasksCommentCmd, "comment")
	tasksCmd.AddCommand(tasksCommentCmd)
}
