package cmd

import (
	"net/http"

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
	Long: `Create a revoke access request.

Prints the created task as pretty JSON, unwrapped like "requests get": the id
is .id. Its .state is a snapshot from creation, not the outcome; re-read with
"c1i requests get <task-id>" before acting on it. --fields is a usage error on
a mutation; extract with jq -er, which fails on a --dry-run preview instead of
printing null.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		entitlementID, _ := cmd.Flags().GetString("entitlement-id")
		userID, _ := cmd.Flags().GetString("user-id")
		description, _ := cmd.Flags().GetString("description")

		body := buildRevokeTaskBody(appID, entitlementID, userID, description)
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: "/api/v1/task/revoke", Body: body}, selfUserOpts(userID)...)
	},
}

func init() {
	requestsCreateRevokeCmd.Flags().String("app-id", "", "Application ID")
	requestsCreateRevokeCmd.Flags().String("entitlement-id", "", "Entitlement ID")
	requestsCreateRevokeCmd.Flags().String("user-id", "", "User ID (defaults to self if omitted)")
	requestsCreateRevokeCmd.Flags().String("description", "", "Justification or description")
	markRequired(requestsCreateRevokeCmd, "app-id")
	markRequired(requestsCreateRevokeCmd, "entitlement-id")
	markMutating(requestsCreateRevokeCmd)
	requestsCreateCmd.AddCommand(requestsCreateRevokeCmd)
}
