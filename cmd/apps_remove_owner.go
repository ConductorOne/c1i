package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var appsRemoveOwnerCmd = &cobra.Command{
	Use:   "remove-owner <user-id>",
	Short: "Remove one owner from an app",
	Long: `Remove a single user as an app owner via DELETE .../owners/{user_id}. Unlike
"apps set-owners", which replaces the whole list with exactly the ids you
pass, this removes one owner without touching the rest of the list -- so it
cannot drop an owner someone else added while you were deciding.

"apps create" auto-assigns its caller as an owner, so removing the last
owner you know about can still leave the app owned by someone you never
chose -- its creator. Check "c1i apps owners <app-id>" first if that matters.

Owner changes are provisioned ASYNCHRONOUSLY: this call returns as soon as
the request is accepted, but the removal takes roughly 45-150s to show
up. Verify with "c1i apps owners <app-id>".

Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id"); err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		appID, _ := cmd.Flags().GetString("app-id")
		userID := args[0]

		path := client.Path("/api/v1/apps/%s/owners/%s", appID, userID)
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
	appsRemoveOwnerCmd.Flags().String("app-id", "", "Application ID to remove the owner from")
	markRequired(appsRemoveOwnerCmd, "app-id")
	appsCmd.AddCommand(appsRemoveOwnerCmd)
}
