package cmd

import (
	"context"
	"net/http"
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
approval step and no access request task. It needs a role that may manage the
entitlement's app. For a grant that goes through policy and approval use
"c1i requests create grant".

The server accepts an unknown user id with 200 and does nothing, so the ids are
looked up first. An unknown entitlement or app exits 4.

The grant is applied asynchronously: it was visible to "c1i grants list" within
seconds in testing, but reads can lag. Pass --wait to block until every user
shows as a grant on the entitlement, or --wait-timeout elapses (a timeout exits
1, though the write was accepted). Prints the API's JSON response, whose
failedUsersErrorMap lists users the server could not add (per the OpenAPI
schema); --wait's progress lines go to stderr.

There is no "grants remove". The only direct removal, DELETE
.../remove-membership, takes one app user id and rejects a manual entitlement
with: entitlement is not an SSO application sign-in entitlement. The
policy-respecting removal is "c1i requests create revoke"; on a manual
entitlement its task waits on a provisioning step (the step reads
PROVISION_INSTANCE_STATE_WAITING) and the grant stays until that step is
completed.

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
			withWait(func(c *client.Client) waitOp[[]string] {
				return grantHoldersWaitOp(c, appID, entitlementID, untilPresent(userIDs), waitTimeout)
			}, wait))
	},
}

// grantHoldersWaitOp polls an entitlement's grants until done accepts the C1
// user ids that hold it.
func grantHoldersWaitOp(c *client.Client, appID, entitlementID string, done func([]string) bool, timeout time.Duration) waitOp[[]string] {
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
