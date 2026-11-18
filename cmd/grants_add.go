package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var grantsAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Grant users a manually managed entitlement directly, bypassing request policy",
	Long: `Add each --user-id as a manually managed member of an entitlement via
POST /api/v1/apps/{app-id}/entitlements/{entitlement-id}/add-manual-user, one
request for all ids. A repeated --user-id is sent once; adding a user who
already holds the entitlement exits 0.

This is a direct grant: it skips the entitlement's grant policy, so there is no
approval step and no access request task. For a grant that goes through policy
and approval use "c1i requests create grant".

The server accepts an unknown user id with 200 and does nothing, so the ids are
looked up first. An unknown entitlement or app exits 4 on a real run;
--dry-run does not look them up.

The grant is applied asynchronously: it was visible to "c1i grants list" within
seconds in testing, but reads can lag. Pass --wait to block until every user
shows as a grant on the entitlement, or --wait-timeout elapses (a timeout exits
1, though the write was accepted). Prints the API's JSON response; --wait's
progress lines go to stderr. If its failedUsersErrorMap (per the OpenAPI schema,
users the server could not add) is not empty, the response is still printed,
each rejected user and its message is reported, and the command exits 1; --wait
then waits only for the users that were not rejected (none left: no wait).

The direct removal, "c1i grants remove", refused manually managed custom and
group entitlements in testing (a success was never exercised) with:
entitlement is not an SSO application sign-in entitlement. The
policy-respecting removal is "c1i requests create revoke"; on a manual
entitlement its task waits on a provisioning step (the step reads
PROVISION_INSTANCE_STATE_WAITING) and the grant stays until that step is
completed. "c1i grants update-duration" sets when C1 starts the revoke.

Honors --dry-run (with --wait, dry-run still only previews the POST; it never
polls).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		entitlementID, _ := cmd.Flags().GetString("entitlement-id")
		userIDs, err := repeatableStringFlag(cmd, "user-id")
		if err != nil {
			return err
		}
		userIDs = dedupe(userIDs)
		wait, waitTimeout, err := waitFlagValues(cmd)
		if err != nil {
			return err
		}
		path := client.Path("/api/v1/apps/%s/entitlements/%s/add-manual-user", appID, entitlementID)
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: map[string]any{"userIds": userIDs}},
			withPreRead(existingUsersCheck("user-id", userIDs)),
			withResponse(func(c *client.Client, resp []byte) writeOutcome {
				rejected := map[string]string{}
				err := rejectedUsersError(resp, rejected)
				return writeOutcome{
					Wait: grantHoldersWaitOp(c, appID, entitlementID, withoutKeys(userIDs, rejected), waitTimeout),
					Err:  err,
				}
			}, wait))
	},
}

// rejectedUsersError reports the users the server refused in a 200 response's
// failedUsersErrorMap, or nil when there are none. The ids and messages are
// copied into rejected.
func rejectedUsersError(data []byte, rejected map[string]string) error {
	var resp struct {
		Failed map[string]string `json:"failedUsersErrorMap"`
	}
	if json.Unmarshal(data, &resp) != nil || len(resp.Failed) == 0 {
		return nil
	}
	ids := make([]string, 0, len(resp.Failed))
	for id, msg := range resp.Failed {
		rejected[id] = msg
		ids = append(ids, id)
	}
	sort.Strings(ids)
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprintf("%s: %s", id, resp.Failed[id])
	}
	return fmt.Errorf("the server did not add %d user(s): %s", len(ids), strings.Join(parts, "; "))
}

// grantHoldersWaitOp polls an entitlement's grants until every want id holds
// it; an empty want returns an op with no Poll, which sendMutation skips.
func grantHoldersWaitOp(c *client.Client, appID, entitlementID string, want []string, timeout time.Duration) waitOp[[]string] {
	if len(want) == 0 {
		return waitOp[[]string]{}
	}
	done := untilPresent(want)
	q := grantsQuery{appID: appID, entitlementID: entitlementID}
	return waitOp[[]string]{
		Poll: func(ctx context.Context) ([]string, error) {
			items, err := fetchAllGrants(ctx, c, q, defaultPageSize)
			if err != nil {
				return nil, err
			}
			ids := make([]string, 0, len(items))
			for _, it := range items {
				ids = append(ids, it.AppEntitlementUserBinding.AppUser.AppUser.IdentityUserID)
			}
			return ids, nil
		},
		Done:     done,
		Interval: grantsWaitPollInterval,
		Timeout:  timeout,
		Subject:  "grants to appear on entitlement " + entitlementID,
		Success:  "Grants appeared on entitlement " + entitlementID,
		Slow:     "grant provisioning can take several minutes",
		Recheck:  "c1i grants list " + strings.Join(q.rescanFlags(), " "),
	}
}

func init() {
	grantsAddCmd.Flags().String("app-id", "", "Application ID the entitlement belongs to")
	grantsAddCmd.Flags().String("entitlement-id", "", "Entitlement ID to grant")
	addRepeatableStringFlag(grantsAddCmd, "user-id", "C1 user ID to grant (repeatable)")
	markRequired(grantsAddCmd, "app-id", "entitlement-id", "user-id")
	addWaitFlags(grantsAddCmd, "GET grants until every user holds the entitlement", 4*time.Minute)
	markMutating(grantsAddCmd)
	markUserRefChecked(grantsAddCmd, "user-id")
	grantsCmd.AddCommand(grantsAddCmd)
}

// withoutKeys returns ids minus those that are keys of drop.
func withoutKeys(ids []string, drop map[string]string) []string {
	var out []string
	for _, id := range ids {
		if _, ok := drop[id]; !ok {
			out = append(out, id)
		}
	}
	return out
}
