package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// newRevokeClient builds the client `requests create revoke` sends its
// self-resolution (currentUserID) and POST /api/v1/task/revoke requests
// through. It's a var, not a direct newClient call, so a test can substitute
// a client pointed at an httptest.Server (via client.NewForTesting) without a
// real OAuth mint — the same DI pattern `c1i api` uses via newAPIClient and
// requests_create_grant.go uses via newGrantClient.
var newRevokeClient = newClient

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

		if dryRunActive() {
			// Resolving "self" costs an authenticated introspect call (see
			// currentUserID), and dry-run is documented/used elsewhere in this
			// CLI as not requiring authentication (every other mutating
			// command checks dryRunActive before newClient). So an omitted
			// --user-id previews with identityUserId absent, same as before
			// this fix — the preview just can't show what "self" resolves to
			// without authenticating.
			body := buildRevokeTaskBody(appID, entitlementID, userID, description)
			return printDryRun(cmd, "POST", "/api/v1/task/revoke", body)
		}

		c, err := newRevokeClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		// --user-id's help promises "defaults to self if omitted" — resolve it
		// here via the same introspect-based lookup requests_list.go's default
		// requester scope and tasks_list.go's --assigned-to-me already use
		// (see currentUserID in cmd/tasks.go), rather than sending no
		// identityUserId at all: the API has no default of its own and
		// rejects that with a 500 "user_id is required" (the same defect
		// requests_create_grant.go had).
		if userID == "" {
			userID, err = currentUserID(cmd.Context(), c)
			if err != nil {
				return err
			}
			if userID == "" {
				return fmt.Errorf("could not determine the current user; pass --user-id")
			}
		}

		body := buildRevokeTaskBody(appID, entitlementID, userID, description)

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
