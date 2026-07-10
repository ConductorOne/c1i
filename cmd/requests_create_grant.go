package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// buildGrantTaskBody builds the CreateGrantTaskRequest wire body for
// POST /api/v1/task/grant (c1.api.task.v1.TaskService.CreateGrantTask).
// The endpoint expects the request fields at the TOP LEVEL — wrapping them
// under a "task" key makes the server reject the body with a 400 "unknown
// field \"task\"". Field names must match the proto: identityUserId (not
// userId) and grantDuration (not duration).
func buildGrantTaskBody(appID, entitlementID, userID, duration, description string, emergency bool) map[string]any {
	body := map[string]any{
		"appId":            appID,
		"appEntitlementId": entitlementID,
	}
	if userID != "" {
		body["identityUserId"] = userID
	}
	if duration != "" {
		body["grantDuration"] = duration
	}
	if description != "" {
		body["description"] = description
	}
	if emergency {
		body["emergencyAccess"] = true
	}
	return body
}

var requestsCreateGrantCmd = &cobra.Command{
	Use:   "grant",
	Short: "Create a grant access request",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		appID, _ := cmd.Flags().GetString("app-id")
		entitlementID, _ := cmd.Flags().GetString("entitlement-id")
		userID, _ := cmd.Flags().GetString("user-id")
		duration, _ := cmd.Flags().GetString("duration")
		description, _ := cmd.Flags().GetString("description")
		emergency, _ := cmd.Flags().GetBool("emergency")

		body := buildGrantTaskBody(appID, entitlementID, userID, duration, description, emergency)

		if dryRunActive() {
			return printDryRun(cmd, "POST", "/api/v1/task/grant", body)
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		data, err := c.Post(cmd.Context(), "/api/v1/task/grant", body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		// CreateGrantTaskResponse nests the created task under
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

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Created grant request: task_id=%s state=%s\n", resp.TaskView.Task.ID, resp.TaskView.Task.State)
		return nil
	},
}

func init() {
	requestsCreateGrantCmd.Flags().String("app-id", "", "Application ID")
	requestsCreateGrantCmd.Flags().String("entitlement-id", "", "Entitlement ID")
	requestsCreateGrantCmd.Flags().String("user-id", "", "User ID (defaults to self if omitted)")
	requestsCreateGrantCmd.Flags().String("duration", "", "Grant duration (e.g. 24h, 7d)")
	requestsCreateGrantCmd.Flags().String("description", "", "Justification or description")
	requestsCreateGrantCmd.Flags().Bool("emergency", false, "Request emergency access")
	markRequired(requestsCreateGrantCmd, "app-id")
	markRequired(requestsCreateGrantCmd, "entitlement-id")
	requestsCreateCmd.AddCommand(requestsCreateGrantCmd)
}
