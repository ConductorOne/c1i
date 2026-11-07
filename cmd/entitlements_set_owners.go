package cmd

import (
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

` + ownerAsyncWaitNote + `
Verify with "c1i entitlements owners <entitlement-id> --app-id <app-id>".

Pass --wait to block and poll GET .../ownerids until the owner set equals
the requested ids -- additions landed and replaced owners gone -- or
--wait-timeout elapses; a timeout exits 1, though the write was accepted.
Prints the API's JSON response; --wait's progress lines go to stderr.

Honors --dry-run (with --wait, dry-run still only previews the PUT; it never
polls).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id"); err != nil {
			return err
		}
		appID, _ := cmd.Flags().GetString("app-id")
		entitlementID := args[0]
		return runOwnerSetPut(cmd, client.Path("/api/v1/apps/%s/entitlements/%s/owners", appID, entitlementID),
			func(c *client.Client, done func([]string) bool, timeout time.Duration) waitOp[[]string] {
				return entitlementOwnersWaitOp(c, appID, entitlementID, done, timeout)
			})
	},
}

func init() {
	entitlementsSetOwnersCmd.Flags().String("app-id", "", "Application ID the entitlement belongs to")
	addRepeatableStringFlag(entitlementsSetOwnersCmd, "user-id", "C1 user ID to set as owner (repeatable; replaces the full owner list)")
	markRequired(entitlementsSetOwnersCmd, "app-id", "user-id")
	addWaitFlags(entitlementsSetOwnersCmd, "GET .../ownerids until the owner set equals the requested ids (removals included)", 4*time.Minute)
	entitlementsCmd.AddCommand(entitlementsSetOwnersCmd)
}
