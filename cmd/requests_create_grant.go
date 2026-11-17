package cmd

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// selfUserOpts resolves a missing --user-id to the caller before the send, and
// looks an explicit one up, on a dry run too, so the preview's body matches
// what would be sent and refuses what a real run refuses.
func selfUserOpts(userID string) []mutationOpt {
	if userID != "" {
		return []mutationOpt{withPreRead(existingUsersCheck("user-id", []string{userID}))}
	}
	return []mutationOpt{withPreRead(preRead{Run: func(ctx context.Context, c *client.Client, m *mutation) ([]resolvedID, error) {
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

An omitted --user-id is the caller.

Prints the created task as pretty JSON, unwrapped like "requests get": the id
is .id. Its .state is a snapshot from creation, not the outcome; re-read with
"c1i requests get <task-id>" before acting on it. --fields is a usage error on
a mutation; extract with jq -er, which fails on a --dry-run preview where jq -r
exits 0 with null.

--wait blocks until the grant this request produces appears in "c1i grants
list" and the matching set stops changing -- the same poll "grants list --wait"
runs. The task is printed as soon as it is created; progress goes to stderr. A
timeout exits 1 even though the request was accepted: provisioning can take
minutes, and a request that still needs approval does not produce the grant
until someone approves it. --wait-min defaults to 1 (unlike "grants list",
where an empty set is a valid answer); --wait-stable and --wait-timeout match
"grants list". --dry-run never polls.

Example:
  TASK_ID=$(c1i requests create grant --app-id "$APP" --entitlement-id "$ENT" | jq -er .id)
  c1i requests create grant --app-id "$APP" --entitlement-id "$ENT" --wait`,
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

		wait, waitTimeout, err := waitFlagValues(cmd)
		if err != nil {
			return err
		}
		stableReads := getIntFlag(cmd, "wait-stable")
		minGrants := getIntFlag(cmd, "wait-min")
		if wait {
			if minGrants < 0 {
				return &usageError{fmt.Errorf("--wait-min cannot be negative")}
			}
			if stableReads < 2 {
				return &usageError{fmt.Errorf("--wait-stable must be at least 2 (one read cannot show that anything held steady)")}
			}
			// The first read is immediate, so n reads need (n-1) intervals.
			if need := time.Duration(stableReads-1) * grantsWaitPollInterval; need >= waitTimeout {
				return &usageError{fmt.Errorf(
					"--wait-stable=%d needs %s at the fixed %s poll interval, which --wait-timeout=%s can never allow; raise --wait-timeout or lower --wait-stable",
					stableReads, need, grantsWaitPollInterval, waitTimeout)}
			}
		}

		body := buildGrantTaskBody(appID, entitlementID, userID, duration, description, emergency)
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: "/api/v1/task/grant", Body: body},
			append(selfUserOpts(userID), withWait(func(c *client.Client) waitOp[[]string] {
				// The pre-read resolved an omitted --user-id into the body; an
				// explicit one is already there. Either way the grant to wait
				// for belongs to it.
				identityUserID, _ := body["identityUserId"].(string)
				if identityUserID == "" {
					identityUserID = userID
				}
				if identityUserID == "" {
					return waitOp[[]string]{
						Poll: func(context.Context) ([]string, error) {
							return nil, &usageError{fmt.Errorf("--wait could not resolve the requesting user; pass --user-id")}
						},
						Done: func([]string) bool { return false },
					}
				}
				return grantsWaitOp(cmd, c, grantsQuery{appID: appID, entitlementID: entitlementID, userID: identityUserID},
					defaultPageSize, stableReads, minGrants, waitTimeout)
			}, wait))...)
	},
}

func init() {
	requestsCreateGrantCmd.Flags().String("app-id", "", "Application ID")
	requestsCreateGrantCmd.Flags().String("entitlement-id", "", "Entitlement ID")
	requestsCreateGrantCmd.Flags().String("user-id", "", "User ID (defaults to self if omitted)")
	addProtoDurationFlag(requestsCreateGrantCmd, "duration",
		"Grant duration as a protobuf duration (86400s) or a Go duration (24h), sent in seconds; a sign or decimal (-1h, 1.5h) exits 2")
	requestsCreateGrantCmd.Flags().String("description", "", "Justification or description")
	allowEmpty(requestsCreateGrantCmd, emptyIsOmitted, "description")
	requestsCreateGrantCmd.Flags().Bool("emergency", false, "Request emergency access")
	addWaitFlags(requestsCreateGrantCmd, "GET /api/v1/search/grants until the requested grant appears and stops changing", 4*time.Minute)
	requestsCreateGrantCmd.Flags().Int("wait-stable", 3, "Consecutive identical reads --wait requires before it settles (min 2)")
	requestsCreateGrantCmd.Flags().Int("wait-min", 1, "Minimum matching grants --wait requires before it settles (default 1; 0 lets an empty result settle)")
	markRequired(requestsCreateGrantCmd, "app-id")
	markRequired(requestsCreateGrantCmd, "entitlement-id")
	markMutating(requestsCreateGrantCmd)
	markUserRefChecked(requestsCreateGrantCmd, "user-id")
	requestsCreateCmd.AddCommand(requestsCreateGrantCmd)
}
