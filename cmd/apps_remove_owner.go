package cmd

import (
	"net/http"

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

` + ownerAsyncNote + `
Verify with "c1i apps owners <app-id>".

Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id"); err != nil {
			return err
		}

		appID, _ := cmd.Flags().GetString("app-id")
		return sendMutation(cmd, mutation{
			Method: http.MethodDelete,
			Path:   client.Path("/api/v1/apps/%s/owners/%s", appID, args[0]),
		})
	},
}

func init() {
	appsRemoveOwnerCmd.Flags().String("app-id", "", "Application ID to remove the owner from")
	markRequired(appsRemoveOwnerCmd, "app-id")
	markMutating(appsRemoveOwnerCmd)
	appsCmd.AddCommand(appsRemoveOwnerCmd)
}
