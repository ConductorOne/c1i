package cmd

import (
	"time"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var entitlementsOwnersCmd = &cobra.Command{
	Use:   "owners <entitlement-id>",
	Short: "List an entitlement's owners (NDJSON output)",
	Long: `List the owners of an app entitlement via GET .../owners -- the read that
reflects what "entitlements set-owners"/"add-owner"/"remove-owner" write.
Owner provisioning is asynchronous: a write showed up here anywhere from
about 1s to 135s after it was accepted, so an owner added moments ago may not
appear yet.

Rows have the same shape as "apps owners".

Auto-paginates to completion like every other list command.

A well-formed entitlement or app id that does not exist returns zero rows and
exit 0, not a 404 -- the endpoint answers 200 with an empty list. An empty
result here is either "no owners" or "wrong id"; "entitlements get" on the
same ids exits 4 and can tell you which.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id"); err != nil {
			return err
		}
		appID, _ := cmd.Flags().GetString("app-id")
		return listOwners(cmd, client.Path("/api/v1/apps/%s/entitlements/%s/owners", appID, args[0]))
	},
}

// entitlementOwnersWaitOp is the --wait of entitlements add-owner and
// set-owners.
func entitlementOwnersWaitOp(c *client.Client, appID, entitlementID string, wantUserIDs []string, timeout time.Duration) waitOp[[]string] {
	return ownerIDsWaitOp(c,
		client.Path("/api/v1/apps/%s/entitlements/%s/ownerids", appID, entitlementID),
		wantUserIDs, timeout,
		"entitlement "+entitlementID+" of app "+appID,
		"c1i entitlements owners "+entitlementID+" --app-id "+appID)
}

// runEntitlementOwnersWait runs the wait with its prose on stderr, so stdout
// stays the write's JSON response.
func runEntitlementOwnersWait(cmd *cobra.Command, c *client.Client, appID, entitlementID string, wantUserIDs []string, timeout time.Duration) error {
	op := entitlementOwnersWaitOp(c, appID, entitlementID, wantUserIDs, timeout)
	op.Out = cmd.ErrOrStderr()
	_, err := runWait(cmd, op)
	return err
}

func init() {
	entitlementsOwnersCmd.Flags().String("app-id", "", "Application ID the entitlement belongs to")
	markRequired(entitlementsOwnersCmd, "app-id")
	addPaginationFlags(entitlementsOwnersCmd)
	entitlementsCmd.AddCommand(entitlementsOwnersCmd)
}
