package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var tasksDenyCmd = &cobra.Command{
	Use:   "deny",
	Short: "Deny an access request task",
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

		body := map[string]any{}
		if comment != "" {
			body["comment"] = comment
		}

		path := fmt.Sprintf("/api/v1/tasks/%s/action/deny", taskID)
		data, err := c.Post(cmd.Context(), path, body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		var resp struct {
			Task struct {
				ID    string `json:"id"`
				State string `json:"state"`
			} `json:"task"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Denied task: task_id=%s state=%s\n", resp.Task.ID, resp.Task.State)
		return nil
	},
}

func init() {
	tasksDenyCmd.Flags().String("task-id", "", "Task ID to deny")
	tasksDenyCmd.Flags().String("comment", "", "Optional comment")
	_ = tasksDenyCmd.MarkFlagRequired("task-id")
	tasksCmd.AddCommand(tasksDenyCmd)
}
