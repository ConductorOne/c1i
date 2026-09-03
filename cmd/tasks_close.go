package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var tasksCloseCmd = &cobra.Command{
	Use:   "close <task-id>",
	Short: "Close a task without approving or denying it",
	Long: `Close a task without approving or denying it.

Closing cancels the task and records no approval decision; use approve/deny to
record an outcome. The confirmation reports only the task id — the action
endpoints echo the task's pre-close state, so printing it would be wrong.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		taskID := args[0]
		comment, _ := cmd.Flags().GetString("comment")

		body := map[string]any{}
		if comment != "" {
			body["comment"] = comment
		}

		path := client.Path("/api/v1/tasks/%s/action/close", taskID)
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

		// State is deliberately not echoed: the action endpoints return the
		// task as it was *before* the action, so a live close prints
		// TASK_STATE_OPEN. Parsing still guards the response shape.
		id, _, err := parseTaskActionResponse(data)
		if err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Closed task: task_id=%s\n", id)
		return nil
	},
}

func init() {
	tasksCloseCmd.Flags().String("comment", "", "Optional comment")
	tasksCmd.AddCommand(tasksCloseCmd)
}
