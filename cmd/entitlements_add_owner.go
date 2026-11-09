package cmd

import (
	"net/http"
	"time"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var entitlementsAddOwnerCmd = &cobra.Command{
	Use:   "add-owner <entitlement-id>",
	Short: "Add one owner to an entitlement",
	Long: `Add the --user-id user as an owner of an app entitlement via POST .../owners.
Unlike "entitlements set-owners", which replaces the whole list with exactly
the ids you pass, this adds one owner without touching the rest of the list.
Adding a user who is already an owner exits 0.

An unknown user, entitlement or app exits 4.

` + ownerAsyncWaitNote + `
Verify with "c1i entitlements owners <entitlement-id> --app-id <app-id>".
--wait polls GET .../ownerids until the owner appears (or --wait-timeout
elapses; a timeout exits 1, though the write was accepted).
Prints the API's JSON response; --wait's progress lines go to stderr.

Honors --dry-run (with --wait, dry-run still only previews the POST; it never
polls).

One owner per call; to change several at once use "entitlements set-owners".`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id", "user-id"); err != nil {
			return err
		}
		wait, waitTimeout, err := waitFlagValues(cmd)
		if err != nil {
			return err
		}

		appID, _ := cmd.Flags().GetString("app-id")
		userID, _ := cmd.Flags().GetString("user-id")
		entitlementID := args[0]

		path := client.Path("/api/v1/apps/%s/entitlements/%s/owners", appID, entitlementID)
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: map[string]any{"userId": userID}},
			withWait(func(c *client.Client) waitOp[[]string] {
				return entitlementOwnersWaitOp(c, appID, entitlementID, untilPresent([]string{userID}), waitTimeout)
			}, wait))
	},
}

func init() {
	entitlementsAddOwnerCmd.Flags().String("app-id", "", "Application ID the entitlement belongs to")
	entitlementsAddOwnerCmd.Flags().String("user-id", "", "C1 user ID to add as owner")
	markRequired(entitlementsAddOwnerCmd, "app-id", "user-id")
	addWaitFlags(entitlementsAddOwnerCmd, "GET .../ownerids until the owner appears", 4*time.Minute)
	markMutating(entitlementsAddOwnerCmd)
	entitlementsCmd.AddCommand(entitlementsAddOwnerCmd)
}
