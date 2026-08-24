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

// setOwnersSuccessFmt is the PUT-accepted confirmation. Names ownerids, not
// "apps get": appOwners was empty on every app checked (see the Long).
const setOwnersSuccessFmt = "Set %d owner(s) on app %s (provisioning is async; check GET .../ownerids in a couple of minutes).\n"

var appsSetOwnersCmd = &cobra.Command{
	Use:   "set-owners <app-id>",
	Short: "Set the owners of an app (replaces the full owner list)",
	Long: `Set the complete list of owners for an app via PUT .../owners, replacing
any existing owners. Provide one or more --user-id (C1 user IDs, 27 chars each).

Owner changes are provisioned ASYNCHRONOUSLY: this call returns immediately,
but the new owners take a couple of minutes to show up in GET .../ownerids.
A success here means the request was accepted, not that the owner list is
already live. The "appOwners" field in "apps get" was observed empty on
every app checked in testing -- don't use it to check ownership.

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
		wait, _ := cmd.Flags().GetBool("wait")
		waitTimeout, _ := cmd.Flags().GetDuration("wait-timeout")
		if wait && waitTimeout <= 0 {
			return &usageError{fmt.Errorf("--wait-timeout must be positive")}
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

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), setOwnersSuccessFmt, len(userIDs), appID)

		if !wait {
			return nil
		}
		return waitForOwners(cmd, c, appID, userIDs, waitTimeout)
	},
}

// waitForOwners polls GET .../ownerids on appID every ownerWaitPollInterval
// until every id in wantUserIDs is present, timeout elapses, or cmd's context
// is canceled. It writes progress lines to cmd's stdout as it goes.
func waitForOwners(cmd *cobra.Command, c *client.Client, appID string, wantUserIDs []string, timeout time.Duration) error {
	out := cmd.OutOrStdout()
	ownerIDsPath := client.Path("/api/v1/apps/%s/ownerids", appID)

	ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
	defer cancel()

	start := time.Now()
	ticker := time.NewTicker(ownerWaitPollInterval)
	defer ticker.Stop()

	// firstPoll suppresses the "still waiting" line on the very first check:
	// that poll happens immediately after the PUT, before any real waiting has
	// elapsed, so printing it there would misleadingly imply time has already
	// passed. Starting with the second poll, real time (>= one tick) has
	// actually elapsed, so the message is accurate.
	firstPoll := true
	for {
		got, err := fetchOwnerIDs(ctx, c, ownerIDsPath)
		if err != nil {
			if ctx.Err() != nil {
				break // fall through to the timeout/cancellation report below
			}
			return fmt.Errorf("API error: %w", err)
		}
		if allOwnersPresent(wantUserIDs, got) {
			_, _ = fmt.Fprintf(out, "Owners provisioned on app %s after %s.\n",
				appID, time.Since(start).Round(time.Second))
			return nil
		}
		if !firstPoll {
			_, _ = fmt.Fprintf(out, "Still waiting for owners to provision on app %s (%s elapsed)...\n",
				appID, time.Since(start).Round(time.Second))
		}
		firstPoll = false

		select {
		case <-ctx.Done():
		case <-ticker.C:
			continue
		}
		break
	}

	if cmd.Context().Err() != nil {
		return fmt.Errorf("canceled while waiting for owners to provision on app %s", appID)
	}
	return fmt.Errorf(
		"timed out after %s waiting for owners to provision on app %s; "+
			"this is not necessarily a failure — provisioning can take several minutes, "+
			"check again later with: c1i api --method GET --path /api/v1/apps/%s/ownerids",
		timeout, appID, appID)
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

// allOwnersPresent reports whether every id in want is present in got. Pure
// (no I/O) so the poll's success condition is unit-testable without a fake
// server: --wait's loop calls this after each GET .../ownerids.
func allOwnersPresent(want, got []string) bool {
	gotSet := make(map[string]struct{}, len(got))
	for _, id := range got {
		gotSet[id] = struct{}{}
	}
	for _, id := range want {
		if _, ok := gotSet[id]; !ok {
			return false
		}
	}
	return true
}

// buildSetOwnersBody assembles the PUT .../owners request body. Pure, so the
// dry-run preview and a unit test pin the exact wire shape (userIds, not
// user_ids, unwrapped) that the API expects.
func buildSetOwnersBody(userIDs []string) map[string]any {
	return map[string]any{"userIds": userIDs}
}

func init() {
	appsSetOwnersCmd.Flags().StringSlice("user-id", nil, "C1 user ID to set as owner (repeatable; replaces the full owner list)")
	markRequired(appsSetOwnersCmd, "user-id")
	appsSetOwnersCmd.Flags().Bool("wait", false, "block and poll GET .../ownerids until the requested owners appear, or --wait-timeout elapses")
	appsSetOwnersCmd.Flags().Duration("wait-timeout", 4*time.Minute, "max time to wait with --wait (e.g. 30s, 5m)")
	appsCmd.AddCommand(appsSetOwnersCmd)
}
