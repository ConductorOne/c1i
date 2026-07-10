package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var tasksApproveCmd = &cobra.Command{
	Use:   "approve",
	Short: "Approve an access request task",
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
		policyStepID, _ := cmd.Flags().GetString("policy-step-id")

		stepID, err := resolvePolicyStepID(cmd.Context(), c, taskID, policyStepID, true)
		if err != nil {
			return err
		}

		body := map[string]any{
			"policyStepId": stepID,
		}
		if comment != "" {
			body["comment"] = comment
		}

		path := client.Path("/api/v1/tasks/%s/action/approve", taskID)
		data, err := c.Post(cmd.Context(), path, body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		id, state, err := parseTaskActionResponse(data)
		if err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Approved task: task_id=%s state=%s\n", id, state)
		return nil
	},
}

func init() {
	tasksApproveCmd.Flags().String("task-id", "", "Task ID to approve")
	tasksApproveCmd.Flags().String("policy-step-id", "", "Policy step to approve (defaults to the task's current step)")
	tasksApproveCmd.Flags().String("comment", "", "Optional comment")
	markRequired(tasksApproveCmd, "task-id")
	tasksCmd.AddCommand(tasksApproveCmd)
}
