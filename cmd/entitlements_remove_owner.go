package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var entitlementsRemoveOwnerCmd = &cobra.Command{
	Use:   "remove-owner <entitlement-id>",
	Short: "Remove one owner from an entitlement",
	Long: `Remove the --user-id user as an owner of an app entitlement via
DELETE .../owners/{user_id}. Unlike "entitlements set-owners", which replaces
the whole list with exactly the ids you pass, this removes one owner without
touching the rest of the list -- so it cannot drop an owner someone else
added while you were deciding.

A user who was never an owner exits 4, the same as a user, entitlement or
app that does not exist, so exit 4 cannot tell "no such user" from "not an
owner". A user who was an owner and has already been removed exits 0.

Unlike "set-owners", which refuses an empty list, this can remove the last
owner.

` + ownerAsyncNote + `
Verify with "c1i entitlements owners <entitlement-id> --app-id <app-id>".

Prints the API's response, which is {}; exit 0 is the success signal.
Honors --dry-run.

One owner per call; to change several at once use "entitlements set-owners".

` + entitlementOwnerRemoveNote,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		appID, _ := cmd.Flags().GetString("app-id")
		if ref, ok, err := ownerEntitlement(cmd, entitlementOwnerV2(appID, args[0])); err != nil || ok {
			if err != nil {
				return err
			}
			return removeEntitlementOwner(cmd, entitlementOwnerV2(appID, args[0]), ref)
		}
		userID, _ := cmd.Flags().GetString("user-id")
		path := client.Path("/api/v1/apps/%s/entitlements/%s/owners/%s", appID, args[0], userID)
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: path})
	},
}

func init() {
	entitlementsRemoveOwnerCmd.Flags().String("app-id", "", "Application ID the entitlement belongs to")
	addOnceStringFlag(entitlementsRemoveOwnerCmd, "user-id", "C1 user ID to remove as owner; this or --owner-entitlement-id is required",
		"use \"entitlements set-owners\" to set several owners")
	addEntitlementOwnerFlags(entitlementsRemoveOwnerCmd, false)
	markRequired(entitlementsRemoveOwnerCmd, "app-id")
	markMutating(entitlementsRemoveOwnerCmd)
	entitlementsCmd.AddCommand(entitlementsRemoveOwnerCmd)
}
