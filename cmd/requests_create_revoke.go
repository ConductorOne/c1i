package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// buildRevokeTaskBody builds the CreateRevokeTaskRequest wire body for
// POST /api/v1/task/revoke (c1.api.task.v1.TaskService.CreateRevokeTask).
// Like the grant endpoint, the fields must be sent at the TOP LEVEL — a
// "task" wrapper is rejected with a 400 "unknown field \"task\"" — and the
// user field is identityUserId, not userId.
func buildRevokeTaskBody(appID, entitlementID, userID, description string) map[string]any {
	body := map[string]any{
		"appId":            appID,
		"appEntitlementId": entitlementID,
	}
	if userID != "" {
		body["identityUserId"] = userID
	}
	if description != "" {
		body["description"] = description
	}
	return body
}

var requestsCreateRevokeCmd = &cobra.Command{
	Use:   "revoke",
	Short: "Create a revoke access request",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		appID, _ := cmd.Flags().GetString("app-id")
		entitlementID, _ := cmd.Flags().GetString("entitlement-id")
		userID, _ := cmd.Flags().GetString("user-id")
		description, _ := cmd.Flags().GetString("description")

		body := buildRevokeTaskBody(appID, entitlementID, userID, description)

		if dryRunActive() {
			return printDryRun(cmd, "POST", "/api/v1/task/revoke", body)
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		data, err := c.Post(cmd.Context(), "/api/v1/task/revoke", body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		// CreateRevokeTaskResponse nests the created task under
		// taskView.task, not at the top level.
		var resp struct {
			TaskView struct {
				Task struct {
					ID    string `json:"id"`
					State string `json:"state"`
				} `json:"task"`
			} `json:"taskView"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Created revoke request: task_id=%s state=%s\n", resp.TaskView.Task.ID, resp.TaskView.Task.State)
		return nil
	},
}

func init() {
	requestsCreateRevokeCmd.Flags().String("app-id", "", "Application ID")
	requestsCreateRevokeCmd.Flags().String("entitlement-id", "", "Entitlement ID")
	requestsCreateRevokeCmd.Flags().String("user-id", "", "User ID (defaults to self if omitted)")
	requestsCreateRevokeCmd.Flags().String("description", "", "Justification or description")
	markRequired(requestsCreateRevokeCmd, "app-id")
	markRequired(requestsCreateRevokeCmd, "entitlement-id")
	requestsCreateCmd.AddCommand(requestsCreateRevokeCmd)
}
