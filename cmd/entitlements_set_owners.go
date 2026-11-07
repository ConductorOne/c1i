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

` + setOwnersLookupNote + `
The API itself accepts an unknown id (exit 0) and drops it: a set naming only
such an id left the entitlement with no owners. The entitlement owners API
answers a repeated id with a 500, so ids are sent once.

` + ownerAsyncWaitNote + `
Verify with "c1i entitlements owners <entitlement-id> --app-id <app-id>".

` + setOwnersWaitNote,
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
	markRequired(entitlementsSetOwnersCmd, "app-id")
	addSetOwnersFlags(entitlementsSetOwnersCmd)
	markMutating(entitlementsSetOwnersCmd)
	entitlementsCmd.AddCommand(entitlementsSetOwnersCmd)
}
