package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var tasksApproveCmd = &cobra.Command{
	Use:   "approve <task-id>",
	Short: "Approve an access request task",
	Long: `Approve an access request task.

The approval targets a specific step of the task's policy via --policy-step-id.
If omitted, the task's currently executing step is fetched and used
automatically; if it cannot be determined the command errors and asks you to
pass --policy-step-id explicitly (approve requires a step).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		taskID := args[0]
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
		if dryRunActive() {
			return printDryRun(cmd, "POST", path, body)
		}
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
	tasksApproveCmd.Flags().String("policy-step-id", "", "Policy step to approve (defaults to the task's current step)")
	tasksApproveCmd.Flags().String("comment", "", "Optional comment")
	tasksCmd.AddCommand(tasksApproveCmd)
}
