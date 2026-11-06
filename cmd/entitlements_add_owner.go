package cmd

import (
	"fmt"
	"time"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var entitlementsAddOwnerCmd = &cobra.Command{
	Use:   "add-owner <user-id>",
	Short: "Add one owner to an entitlement",
	Long: `Add a single user as an owner of an app entitlement via POST .../owners.
Unlike "entitlements set-owners", which replaces the whole list with exactly
the ids you pass, this adds one owner without touching the rest of the list.
Adding a user who is already an owner exits 0.

A user id that does not exist exits 4, as does an entitlement or app that
does not exist.

Owner changes are provisioned ASYNCHRONOUSLY: this call returns as soon as
the request is accepted, and the new owner showed up anywhere from about 1s
to 135s later. Verify with "c1i entitlements owners <entitlement-id> --app-id
<app-id>", or pass --wait to poll GET .../ownerids until the owner appears
(or --wait-timeout elapses). Prints the API's JSON response; --wait's
progress lines go to stderr.

Honors --dry-run (with --wait, dry-run still only previews the POST; it never
polls).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id", "entitlement-id"); err != nil {
			return err
		}
		wait, waitTimeout, err := waitFlagValues(cmd)
		if err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		appID, _ := cmd.Flags().GetString("app-id")
		entitlementID, _ := cmd.Flags().GetString("entitlement-id")
		userID := args[0]

		path := client.Path("/api/v1/apps/%s/entitlements/%s/owners", appID, entitlementID)
		body := map[string]any{"userId": userID}
		if dryRunActive() {
			return printDryRun(cmd, "POST", path, body)
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Post(cmd.Context(), path, body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		if err := writeRawObject(cmd, data); err != nil {
			return err
		}

		if !wait {
			return nil
		}
		return runEntitlementOwnersWait(cmd, c, appID, entitlementID, untilPresent([]string{userID}), waitTimeout)
	},
}

func init() {
	entitlementsAddOwnerCmd.Flags().String("app-id", "", "Application ID the entitlement belongs to")
	entitlementsAddOwnerCmd.Flags().String("entitlement-id", "", "Entitlement ID to add the owner to")
	markRequired(entitlementsAddOwnerCmd, "app-id", "entitlement-id")
	addWaitFlags(entitlementsAddOwnerCmd, "GET .../ownerids until the owner appears", 4*time.Minute)
	entitlementsCmd.AddCommand(entitlementsAddOwnerCmd)
}
