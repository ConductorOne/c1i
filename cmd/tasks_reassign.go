package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var tasksReassignCmd = &cobra.Command{
	Use:   "reassign <task-id>",
	Short: "Reassign a task's approval step to other users",
	Long: `Reassign a task's approval step to one or more other users.

--to-user-id sets the step's approvers to the users named; repeat it to
assign several.

--policy-step-id targets a specific step of the task's policy. If omitted, the
task's currently executing step is fetched and used; if it cannot be determined
the command errors and asks you to pass --policy-step-id explicitly.

The confirmation reports the task id and the policy step acted on, never a
state: the action endpoints echo the task's state from before the action.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		toUserIDs, _ := cmd.Flags().GetStringSlice("to-user-id")
		// Cobra's required check only proves the flag was set. `--to-user-id ""`
		// parses to an empty slice; `a,,b` yields a blank element. Either would
		// otherwise post an empty approver id.
		if len(toUserIDs) == 0 {
			return &usageError{fmt.Errorf("flag --to-user-id requires at least one value")}
		}
		for _, id := range toUserIDs {
			if id == "" {
				return &usageError{fmt.Errorf("flag --to-user-id requires a non-empty value")}
			}
		}

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

		// The API does not require policyStepId here (approve does). We require a
		// resolvable step anyway: a reassign with no step is ambiguous, and failing
		// loudly beats sending it.
		stepID, err := resolvePolicyStepID(cmd.Context(), c, taskID, policyStepID, true)
		if err != nil {
			return err
		}

		body := map[string]any{
			"newStepUserIds": toUserIDs,
			"policyStepId":   stepID,
		}
		if comment != "" {
			body["comment"] = comment
		}

		path := client.Path("/api/v1/tasks/%s/action/reassign", taskID)
		if dryRunActive() {
			return printDryRun(cmd, "POST", path, body)
		}
		data, err := c.Post(cmd.Context(), path, body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		id, _, err := parseTaskActionResponse(data)
		if err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Reassigned task: task_id=%s policy_step_id=%s\n", id, stepID)
		return nil
	},
}

func init() {
	tasksReassignCmd.Flags().StringSlice("to-user-id", nil, "User ID to reassign the step to (repeatable)")
	tasksReassignCmd.Flags().String("policy-step-id", "", "Policy step to reassign (defaults to the task's current step)")
	tasksReassignCmd.Flags().String("comment", "", "Optional comment")
	markRequired(tasksReassignCmd, "to-user-id")
	tasksCmd.AddCommand(tasksReassignCmd)
}
