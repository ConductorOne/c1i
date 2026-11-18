package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// ownerWaitInterval is how often --wait re-polls GET .../ownerids: slow
// enough not to hammer the API through a minutes-long provisioning window,
// fast enough that --wait-timeout stays responsive. A var so a test can poll
// fast.
var ownerWaitInterval = 12 * time.Second

// Owner help text and the --wait timeout state provisioning lag only through
// these, with no measured range: it varies by tenant and server version.
// README.md, cmd/agents.md and the docs guide quote ownerAsyncWaitNote
// (TestOwnerAsyncNoteInDocs).
const (
	ownerAsyncLag      = "can take several minutes to appear"
	ownerAsyncClause   = "Owner changes are applied asynchronously and " + ownerAsyncLag
	ownerAsyncNote     = ownerAsyncClause + "."
	ownerAsyncWaitNote = ownerAsyncClause + "; pass --wait to block until they do."
)

// The paragraphs both set-owners commands share, so the families can't drift.
const (
	setOwnersLookupNote = `A repeated --user-id is sent once.`

	setOwnersWaitNote = `Pass --wait to block and poll GET .../ownerids until the owner set equals
the requested ids -- additions landed and replaced owners gone -- or
--wait-timeout elapses; a timeout exits 1, though the write was accepted.
Prints the API's JSON response; --wait's progress lines go to stderr.

Honors --dry-run (with --wait, dry-run still only previews the PUT; it never
polls).`
)

var appsSetOwnersCmd = &cobra.Command{
	Use:   "set-owners <app-id>",
	Short: "Set the owners of an app (replaces the full owner list)",
	Long: `Set the complete list of owners for an app via PUT .../owners, replacing
any existing owners. Provide one or more --user-id (C1 user IDs, 27 chars each).

` + setOwnersLookupNote + `
The API itself accepts an unknown user id (exit 0) and stores it: GET
.../ownerids lists it but "apps owners" does not, so a set naming only such ids
leaves the app with no real owners. c1i reads the app first: an unknown app id
exits 4 and a deleted one 2, before anything is sent, --dry-run included.

` + ownerAsyncWaitNote + `
Verify with "c1i apps owners <app-id>". Don't check ownership via the
"appOwners" field in "apps get": it read [] on every app checked, including
those GET .../ownerids reported owners for.

` + entitlementOwnerSetNote + `

` + setOwnersWaitNote,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appID := args[0]
		return runOwnerSetPut(cmd, client.Path("/api/v1/apps/%s/owners", appID), appOwnerV2(appID),
			func(c *client.Client, done func([]string) bool, timeout time.Duration) waitOp[[]string] {
				return appOwnersWaitOp(c, appID, done, timeout)
			},
			liveAppPreRead(appID, "<app-id>"))
	},
}

// runOwnerSetPut is the body of every set-owners command: it PUTs the
// --user-id list to path, prints the response, and with --wait polls the
// op waitFor builds until the owner set equals the request. extra adds
// pre-send reads of the owned object. With --owner-entitlement-id it sets both
// owner lists through v2 instead.
func runOwnerSetPut(cmd *cobra.Command, path string, v2 ownerV2, waitFor func(*client.Client, func([]string) bool, time.Duration) waitOp[[]string], extra ...mutationOpt) error {
	userIDs, err := repeatableStringFlag(cmd, "user-id")
	if err != nil {
		return err
	}
	entitlementIDs, err := repeatableStringFlag(cmd, "owner-entitlement-id")
	if err != nil {
		return err
	}
	noUsers, _ := cmd.Flags().GetBool("no-user-owners")
	switch {
	case noUsers && len(entitlementIDs) == 0:
		return &usageError{fmt.Errorf("--no-user-owners requires --owner-entitlement-id")}
	case noUsers && len(userIDs) > 0:
		return &usageError{fmt.Errorf("--no-user-owners and --user-id are mutually exclusive")}
	case len(userIDs) == 0 && len(entitlementIDs) == 0:
		return &usageError{fmt.Errorf("at least one --user-id or --owner-entitlement-id is required")}
	case len(userIDs) == 0 && !noUsers:
		return &usageError{fmt.Errorf("--owner-entitlement-id also replaces user owners: repeat --user-id for each user owner to keep, or pass --no-user-owners to remove them all; to add one entitlement owner use add-owner")}
	}
	// The entitlement owners PUT 500s on a repeated id.
	userIDs = dedupe(userIDs)
	wait, waitTimeout, err := waitFlagValues(cmd)
	if err != nil {
		return err
	}
	if len(entitlementIDs) > 0 {
		return setOwnersV2(cmd, v2, userIDs, dedupe(entitlementIDs), wait, waitTimeout, extra...)
	}
	if _, err := entitlementAppID(cmd, ""); err != nil {
		return err
	}
	opts := append(append([]mutationOpt{}, extra...),
		withPreRead(existingUsersCheck("user-id", userIDs)),
		withWait(func(c *client.Client) waitOp[[]string] { return waitFor(c, untilEqual(userIDs), waitTimeout) }, wait))
	return sendMutation(cmd, mutation{Method: http.MethodPut, Path: path, Body: buildSetOwnersBody(userIDs)}, opts...)
}

// appOwnersWaitOp polls an app's ownerids until done accepts them.
func appOwnersWaitOp(c *client.Client, appID string, done func([]string) bool, timeout time.Duration) waitOp[[]string] {
	return ownerIDsWaitOp(c, client.Path("/api/v1/apps/%s/ownerids", appID), done, timeout,
		"app "+appID, "c1i apps owners "+appID)
}

// runOwnerIDsWait runs an owner write's --wait with its prose on stderr, so
// stdout stays the write's JSON response.
func runOwnerIDsWait(cmd *cobra.Command, op waitOp[[]string]) error {
	op.Out = cmd.ErrOrStderr()
	_, err := runWait(cmd, op)
	return err
}

// ownerIDsWaitOp polls an .../ownerids path (an app's or an entitlement's)
// until done accepts the owner ids (untilPresent or untilEqual). target names
// the owned object in the progress lines ("app X"); recheck is the read to
// suggest on timeout.
func ownerIDsWaitOp(c *client.Client, ownerIDsPath string, done func([]string) bool, timeout time.Duration, target, recheck string) waitOp[[]string] {
	return waitOp[[]string]{
		Poll: func(ctx context.Context) ([]string, error) {
			got, err := fetchOwnerIDs(ctx, c, ownerIDsPath)
			if err != nil {
				return nil, fmt.Errorf("API error: %w", err)
			}
			return got, nil
		},
		Done:     done,
		Interval: ownerWaitInterval,
		Timeout:  timeout,
		Subject:  "owners to provision on " + target,
		Success:  "Owners provisioned on " + target,
		Slow:     "owner changes " + ownerAsyncLag,
		Recheck:  recheck,
	}
}

// fetchOwnerIDs GETs .../ownerids and returns the current owner user IDs.
func fetchOwnerIDs(ctx context.Context, c *client.Client, path string) ([]string, error) {
	data, err := c.Get(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		UserIDs []string `json:"userIds"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("parsing ownerids response: %w", err)
	}
	return parsed.UserIDs, nil
}

// buildSetOwnersBody assembles the PUT .../owners request body. Pure, so the
// dry-run preview and a unit test pin the exact wire shape (userIds, not
// user_ids, unwrapped) that the API expects.
func buildSetOwnersBody(userIDs []string) map[string]any {
	return map[string]any{"userIds": userIDs}
}

// addSetOwnersFlags registers the flags every set-owners command shares.
func addSetOwnersFlags(cmd *cobra.Command) {
	addRepeatableStringFlag(cmd, "user-id", "C1 user ID to set as owner (repeatable; replaces the user owners); this or --owner-entitlement-id is required")
	addEntitlementOwnerFlags(cmd, true)
	cmd.Flags().Bool("no-user-owners", false, "With --owner-entitlement-id and no --user-id, remove every user owner")
	addWaitFlags(cmd, "GET .../ownerids (with --owner-entitlement-id, also the entitlement owner list) until the owners equal the request (removals included)", 4*time.Minute)
}

func init() {
	addSetOwnersFlags(appsSetOwnersCmd)
	markMutating(appsSetOwnersCmd)
	markUserRefChecked(appsSetOwnersCmd, "user-id")
	appsCmd.AddCommand(appsSetOwnersCmd)
}
