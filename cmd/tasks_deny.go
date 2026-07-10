package cmd

import (
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

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		taskID, _ := cmd.Flags().GetString("task-id")
		comment, _ := cmd.Flags().GetString("comment")
		policyStepID, _ := cmd.Flags().GetString("policy-step-id")

		// policyStepId is optional for deny; include it when we can target a
		// specific step (needed on multi-step policies) but don't require one.
		stepID, err := resolvePolicyStepID(cmd.Context(), c, taskID, policyStepID, false)
		if err != nil {
			return err
		}

		body := map[string]any{}
		if stepID != "" {
			body["policyStepId"] = stepID
		}
		if comment != "" {
			body["comment"] = comment
		}

		path := client.Path("/api/v1/tasks/%s/action/deny", taskID)
		data, err := c.Post(cmd.Context(), path, body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		id, state, err := parseTaskActionResponse(data)
		if err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Denied task: task_id=%s state=%s\n", id, state)
		return nil
	},
}

func init() {
	tasksDenyCmd.Flags().String("task-id", "", "Task ID to deny")
	tasksDenyCmd.Flags().String("policy-step-id", "", "Policy step to deny (defaults to the task's current step)")
	tasksDenyCmd.Flags().String("comment", "", "Optional comment")
	markRequired(tasksDenyCmd, "task-id")
	tasksCmd.AddCommand(tasksDenyCmd)
}
