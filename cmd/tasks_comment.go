package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var tasksCommentCmd = &cobra.Command{
	Use:   "comment <task-id>",
	Short: "Add a comment to a task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		taskID := args[0]
		comment, _ := cmd.Flags().GetString("comment")

		body := map[string]any{
			"comment": comment,
		}

		path := client.Path("/api/v1/tasks/%s/action/comment", taskID)
		if dryRunActive() {
			return printDryRun(cmd, "POST", path, body)
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
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
	tasksCommentCmd.Flags().String("comment", "", "Comment text")
	markRequired(tasksCommentCmd, "comment")
	tasksCmd.AddCommand(tasksCommentCmd)
}
