package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// ownerWaitInterval is how often --wait re-polls GET .../ownerids. Long
// enough to avoid hammering the API during a provisioning window observed
// at 96-129s across four converged writes (a fifth was still pending at
// 108s), short enough that --wait-timeout still feels responsive. A var so a
// test can poll fast.
var ownerWaitInterval = 12 * time.Second

var appsSetOwnersCmd = &cobra.Command{
	Use:   "set-owners <app-id>",
	Short: "Set the owners of an app (replaces the full owner list)",
	Long: `Set the complete list of owners for an app via PUT .../owners, replacing
any existing owners. Provide one or more --user-id (C1 user IDs, 27 chars each).

Before the PUT, each distinct --user-id is looked up with GET
/api/v1/users/{id}; an unknown one exits 2 naming it, and nothing is sent.
The API itself accepts an unknown id (exit 0) and stores it: GET .../ownerids
lists it but "apps owners" does not, so a set naming only such ids leaves the
app with no real owners. --dry-run skips the lookups. A repeated --user-id
is sent once.

Owner changes are provisioned ASYNCHRONOUSLY: this call returns as soon as
the request is accepted, and the change showed up anywhere from about 1s to
135s later. Verify with "c1i apps owners <app-id>". Don't check
ownership via the "appOwners" field in "apps get": it read [] on every app
checked, including those GET .../ownerids reported owners for.

Pass --wait to block and poll GET .../ownerids until the owner set equals
the requested ids -- additions landed and replaced owners gone -- or
--wait-timeout elapses; a timeout exits 1, though the write was accepted.
Prints the API's JSON response; --wait's progress lines go to stderr.

Honors --dry-run (with --wait, dry-run still only previews the PUT; it never
polls).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appID := args[0]
		return runOwnerSetPut(cmd, client.Path("/api/v1/apps/%s/owners", appID),
			func(c *client.Client, done func([]string) bool, timeout time.Duration) waitOp[[]string] {
				return appOwnersWaitOp(c, appID, done, timeout)
			})
	},
}

// runOwnerSetPut is the body of every set-owners command: it PUTs the
// --user-id list to path, prints the response, and with --wait polls the
// op waitFor builds until the owner set equals the request.
func runOwnerSetPut(cmd *cobra.Command, path string, waitFor func(*client.Client, func([]string) bool, time.Duration) waitOp[[]string]) error {
	userIDs, err := repeatableStringFlag(cmd, "user-id")
	if err != nil {
		return err
	}
	if len(userIDs) == 0 {
		return &usageError{fmt.Errorf("at least one --user-id is required")}
	}
	// The entitlement owners PUT 500s on a repeated id.
	userIDs = dedupe(userIDs)
	wait, waitTimeout, err := waitFlagValues(cmd)
	if err != nil {
		return err
	}

	baseURL, err := GetBaseURL()
	if err != nil {
		return err
	}

	body := buildSetOwnersBody(userIDs)
	if dryRunActive() {
		return printDryRun(cmd, "PUT", path, body)
	}

	c, err := newClient(cmd, baseURL)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	if err := requireExistingUsers(cmd.Context(), c, "user-id", userIDs); err != nil {
		return err
	}
	data, err := c.Put(cmd.Context(), path, body)
	if err != nil {
		return fmt.Errorf("API error: %w", err)
	}
	if err := writeRawObject(cmd, data); err != nil {
		return err
	}

	if !wait {
		return nil
	}
	return runOwnerIDsWait(cmd, waitFor(c, untilEqual(userIDs), waitTimeout))
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
		Slow:     "provisioning can take several minutes",
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

func init() {
	addRepeatableStringFlag(appsSetOwnersCmd, "user-id", "C1 user ID to set as owner (repeatable; replaces the full owner list)")
	markRequired(appsSetOwnersCmd, "user-id")
	addWaitFlags(appsSetOwnersCmd, "GET .../ownerids until the owner set equals the requested ids (removals included)", 4*time.Minute)
	appsCmd.AddCommand(appsSetOwnersCmd)
}
