package cmd

import (
	"context"
	"fmt"
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// selfUserOpts resolves a missing --user-id to the caller before the send, on
// a dry run too, so the preview's body matches what would be sent. An explicit
// --user-id needs no read, so its preview needs no credentials.
func selfUserOpts(userID string) []mutationOpt {
	if userID != "" {
		return nil
	}
	return []mutationOpt{withPreRead(preRead{OnDryRun: true, Run: func(ctx context.Context, c *client.Client, m *mutation) ([]resolvedID, error) {
		id, err := currentUserID(ctx, c)
		if err != nil {
			return nil, err
		}
		if id == "" {
			return nil, &usageError{fmt.Errorf("could not determine the current user; pass --user-id")}
		}
		m.Body.(map[string]any)["identityUserId"] = id
		return nil, nil
	}})}
}

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
	Long: `Create a grant access request.

--duration takes a whole-number Go (24h, 1h30m) or protobuf (86400s) duration
(no sign, no decimals) and is sent in seconds, the only form the API parses;
anything else, such as -1h, 1.5h or 7d, exits 2 before sending. Zero is sent,
and the server refuses it with "value must be greater than 0s".

Prints the created task as pretty JSON, unwrapped like "requests get": the id
is .id. Its .state is a snapshot from creation, not the outcome; re-read with
"c1i requests get <task-id>" before acting on it. --fields is a usage error on
a mutation; extract with jq -er, which fails on a --dry-run preview instead of
printing null.

Example:
  TASK_ID=$(c1i requests create grant --app-id "$APP" --entitlement-id "$ENT" | jq -er .id)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		entitlementID, _ := cmd.Flags().GetString("entitlement-id")
		userID, _ := cmd.Flags().GetString("user-id")
		duration, _, err := protoDurationFlag(cmd, "duration")
		if err != nil {
			return err
		}
		description, _ := cmd.Flags().GetString("description")
		emergency, _ := cmd.Flags().GetBool("emergency")

		body := buildGrantTaskBody(appID, entitlementID, userID, duration, description, emergency)
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: "/api/v1/task/grant", Body: body}, selfUserOpts(userID)...)
	},
}

func init() {
	requestsCreateGrantCmd.Flags().String("app-id", "", "Application ID")
	requestsCreateGrantCmd.Flags().String("entitlement-id", "", "Entitlement ID")
	requestsCreateGrantCmd.Flags().String("user-id", "", "User ID (defaults to self if omitted)")
	addProtoDurationFlag(requestsCreateGrantCmd, "duration",
		"Grant duration as a protobuf duration (86400s) or a Go duration (24h), sent in seconds; a sign or decimal (-1h, 1.5h) exits 2")
	requestsCreateGrantCmd.Flags().String("description", "", "Justification or description")
	requestsCreateGrantCmd.Flags().Bool("emergency", false, "Request emergency access")
	markRequired(requestsCreateGrantCmd, "app-id")
	markRequired(requestsCreateGrantCmd, "entitlement-id")
	markMutating(requestsCreateGrantCmd)
	requestsCreateCmd.AddCommand(requestsCreateGrantCmd)
}
