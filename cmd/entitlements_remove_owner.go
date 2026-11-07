package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var entitlementsRemoveOwnerCmd = &cobra.Command{
	Use:   "remove-owner <user-id>",
	Short: "Remove one owner from an entitlement",
	Long: `Remove a single user as an owner of an app entitlement via
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

Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id", "entitlement-id"); err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		appID, _ := cmd.Flags().GetString("app-id")
		entitlementID, _ := cmd.Flags().GetString("entitlement-id")

		path := client.Path("/api/v1/apps/%s/entitlements/%s/owners/%s", appID, entitlementID, args[0])
		if dryRunActive() {
			return printDryRun(cmd, "DELETE", path, nil)
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Delete(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeRawObject(cmd, data)
	},
}

func init() {
	entitlementsRemoveOwnerCmd.Flags().String("app-id", "", "Application ID the entitlement belongs to")
	entitlementsRemoveOwnerCmd.Flags().String("entitlement-id", "", "Entitlement ID to remove the owner from")
	markRequired(entitlementsRemoveOwnerCmd, "app-id", "entitlement-id")
	entitlementsCmd.AddCommand(entitlementsRemoveOwnerCmd)
}
