package cmd

import (
	"fmt"
	"time"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var entitlementsSetOwnersCmd = &cobra.Command{
	Use:   "set-owners <entitlement-id>",
	Short: "Set the owners of an entitlement (replaces the full owner list)",
	Long: `Set the complete list of owners for an app entitlement via PUT .../owners,
replacing any existing owners. Provide one or more --user-id.

At least one --user-id is required: the API takes an empty list as "remove
every owner", which is too easy to send by accident with an unset shell
variable. Use "entitlements remove-owner" to drop owners one at a time.

Before the PUT, each distinct --user-id is looked up with GET
/api/v1/users/{id}; an unknown one exits 2 naming it, and nothing is sent.
The API itself accepts an unknown id (exit 0) and drops it: a set naming only
such an id left the entitlement with no owners. --dry-run skips the lookups.
A repeated --user-id is sent once: the API answers a duplicate with a 500.

Owner changes are provisioned ASYNCHRONOUSLY: this call returns as soon as
the request is accepted, and the change showed up anywhere from about 1s to
135s later. Verify with "c1i entitlements owners <entitlement-id> --app-id
<app-id>".

Pass --wait to block and poll GET .../ownerids until the owner set equals
the requested ids -- additions landed and replaced owners gone -- or
--wait-timeout elapses. Prints the API's JSON response; --wait's progress
lines go to stderr.

Honors --dry-run (with --wait, dry-run still only previews the PUT; it never
polls).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id"); err != nil {
			return err
		}
		userIDs, err := repeatableStringFlag(cmd, "user-id")
		if err != nil {
			return err
		}
		if len(userIDs) == 0 {
			return &usageError{fmt.Errorf("at least one --user-id is required")}
		}
		// The API 500s on a repeated id.
		userIDs = dedupe(userIDs)
		wait, waitTimeout, err := waitFlagValues(cmd)
		if err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		appID, _ := cmd.Flags().GetString("app-id")
		entitlementID := args[0]
		path := client.Path("/api/v1/apps/%s/entitlements/%s/owners", appID, entitlementID)
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
		return runEntitlementOwnersWait(cmd, c, appID, entitlementID, untilEqual(userIDs), waitTimeout)
	},
}

func init() {
	entitlementsSetOwnersCmd.Flags().String("app-id", "", "Application ID the entitlement belongs to")
	addRepeatableStringFlag(entitlementsSetOwnersCmd, "user-id", "C1 user ID to set as owner (repeatable; replaces the full owner list)")
	markRequired(entitlementsSetOwnersCmd, "app-id", "user-id")
	addWaitFlags(entitlementsSetOwnersCmd, "GET .../ownerids until the owner set equals the requested ids (removals included)", 4*time.Minute)
	entitlementsCmd.AddCommand(entitlementsSetOwnersCmd)
}
