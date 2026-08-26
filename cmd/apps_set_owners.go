package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// ownerWaitPollInterval is how often --wait re-polls GET .../ownerids. Long
// enough to avoid hammering the API during a provisioning window observed
// at 96-129s across four converged writes (a fifth was still pending at
// 108s), short enough that --wait-timeout still feels responsive.
const ownerWaitPollInterval = 12 * time.Second

// setOwnersSuccessFmt is the PUT-accepted confirmation. Points at "apps
// owners", not "apps get": appOwners was [] on all 46 apps measured, 45 of
// which did have owners.
const setOwnersSuccessFmt = "Set %d owner(s) on app %s (provisioning is async; check with \"c1i apps owners %s\" in a minute or two).\n"

var appsSetOwnersCmd = &cobra.Command{
	Use:   "set-owners <app-id>",
	Short: "Set the owners of an app (replaces the full owner list)",
	Long: `Set the complete list of owners for an app via PUT .../owners, replacing
any existing owners. Provide one or more --user-id (C1 user IDs, 27 chars each).

Owner changes are provisioned ASYNCHRONOUSLY: this call returns immediately,
but the new owners take a couple of minutes to show up in GET .../ownerids.
A success here means the request was accepted, not that the owner list is
already live. Don't check ownership via the "appOwners" field in "apps get":
it was [] on all 46 apps in the test tenant, including the 45 that
GET .../ownerids reported owners for.

Pass --wait to block and poll GET .../ownerids until every requested
--user-id appears (or --wait-timeout elapses). Without --wait, behavior is
unchanged: the command returns as soon as the PUT is accepted.

Honors --dry-run (with --wait, dry-run still only previews the PUT; it never
polls).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		userIDs, _ := cmd.Flags().GetStringSlice("user-id")
		if len(userIDs) == 0 {
			return &usageError{fmt.Errorf("at least one --user-id is required")}
		}
		for _, id := range userIDs {
			if strings.TrimSpace(id) == "" {
				// An empty id would send userIds:[""] and earn a confusing 4xx
				// (the API requires a 27-char user id); reject it up front.
				return &usageError{fmt.Errorf("--user-id values must be non-empty")}
			}
		}
		wait, waitTimeout, err := waitFlagValues(cmd)
		if err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		appID := args[0]
		path := client.Path("/api/v1/apps/%s/owners", appID)
		body := buildSetOwnersBody(userIDs)
		if dryRunActive() {
			return printDryRun(cmd, "PUT", path, body)
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		if _, err := c.Put(cmd.Context(), path, body); err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), setOwnersSuccessFmt, len(userIDs), appID, appID)

		if !wait {
			return nil
		}
		return waitForOwners(cmd, c, appID, userIDs, waitTimeout)
	},
}

// waitForOwners blocks until every id in wantUserIDs appears in
// GET .../ownerids on appID, timeout elapses, or cmd's context is canceled.
func waitForOwners(cmd *cobra.Command, c *client.Client, appID string, wantUserIDs []string, timeout time.Duration) error {
	return runWait(cmd, ownersWaitOp(c, appID, wantUserIDs, timeout))
}

// ownersWaitOp is waitForOwners' operation, built separately so a test can
// drive the loop at a poll interval shorter than ownerWaitPollInterval.
func ownersWaitOp(c *client.Client, appID string, wantUserIDs []string, timeout time.Duration) waitOp[[]string] {
	ownerIDsPath := client.Path("/api/v1/apps/%s/ownerids", appID)
	return waitOp[[]string]{
		Poll: func(ctx context.Context) ([]string, error) {
			got, err := fetchOwnerIDs(ctx, c, ownerIDsPath)
			if err != nil {
				return nil, fmt.Errorf("API error: %w", err)
			}
			return got, nil
		},
		Done:     untilPresent(wantUserIDs),
		Interval: ownerWaitPollInterval,
		Timeout:  timeout,
		Subject:  fmt.Sprintf("owners to provision on app %s", appID),
		Success:  fmt.Sprintf("Owners provisioned on app %s", appID),
		Slow:     "provisioning can take several minutes",
		Recheck:  fmt.Sprintf("c1i apps owners %s", appID),
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

// allOwnersPresent reports whether every id in want is present in got: the
// set-owners --wait success condition (untilPresent's) as a pure function, so
// it stays unit-testable without a fake server.
func allOwnersPresent(want, got []string) bool { return untilPresent(want)(got) }

// buildSetOwnersBody assembles the PUT .../owners request body. Pure, so the
// dry-run preview and a unit test pin the exact wire shape (userIds, not
// user_ids, unwrapped) that the API expects.
func buildSetOwnersBody(userIDs []string) map[string]any {
	return map[string]any{"userIds": userIDs}
}

func init() {
	appsSetOwnersCmd.Flags().StringSlice("user-id", nil, "C1 user ID to set as owner (repeatable; replaces the full owner list)")
	markRequired(appsSetOwnersCmd, "user-id")
	addWaitFlags(appsSetOwnersCmd, "GET .../ownerids until the requested owners appear", 4*time.Minute)
	appsCmd.AddCommand(appsSetOwnersCmd)
}
