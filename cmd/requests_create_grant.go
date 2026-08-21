package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// newGrantClient builds the client `requests create grant` sends its
// self-resolution (currentUserID) and POST /api/v1/task/grant requests
// through. It's a var, not a direct newClient call, so a test can substitute
// a client pointed at an httptest.Server (via client.NewForTesting) without a
// real OAuth mint — the same DI pattern `c1i api` uses via newAPIClient.
var newGrantClient = newClient

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

		// Authenticate before the dry-run check (like tasks_approve.go /
		// tasks_deny.go resolving the policy step before theirs) so that when
		// --user-id is omitted, self-resolution below runs before printDryRun
		// builds the body — otherwise --dry-run would preview a body missing
		// identityUserId while the real call sends one. This means --dry-run
		// now needs credentials when --user-id is omitted; see README.md's
		// "Dry run" section for the exception list.
		c, err := newGrantClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		// --user-id's help promises "defaults to self if omitted" — resolve it
		// here via the same introspect-based lookup requests_list.go's default
		// requester scope and tasks_list.go's --assigned-to-me already use
		// (see currentUserID in cmd/tasks.go), rather than sending no
		// identityUserId at all: the API has no default of its own and
		// rejects that with a 500 "user_id is required". Skipped entirely when
		// --user-id is explicit, so that path never pays for the introspect
		// call, dry-run or not.
		if userID == "" {
			userID, err = currentUserID(cmd.Context(), c)
			if err != nil {
				return err
			}
			if userID == "" {
				return &usageError{fmt.Errorf("could not determine the current user; pass --user-id")}
			}
		}

		body := buildGrantTaskBody(appID, entitlementID, userID, duration, description, emergency)

		if dryRunActive() {
			return printDryRun(cmd, "POST", "/api/v1/task/grant", body)
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
