package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var appsRemoveOwnerCmd = &cobra.Command{
	Use:   "remove-owner <app-id>",
	Short: "Remove one owner from an app",
	Long: `Remove the --user-id user as an owner of an app via DELETE .../owners/{user_id}. Unlike
"apps set-owners", which replaces the whole list with exactly the ids you
pass, this removes one owner without touching the rest of the list -- so it
cannot drop an owner someone else added while you were deciding.

"apps create" auto-assigns its caller as an owner, so removing the last
owner you know about can still leave the app owned by someone you never
chose -- its creator. Check "c1i apps owners <app-id>" first if that matters.

` + ownerAsyncNote + `
An unknown app id exits 4.
Verify with "c1i apps owners <app-id>".

Prints the API's response, which is {}; exit 0 is the success signal.
Honors --dry-run.

One owner per call; to change several at once use "apps set-owners".`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		userID, _ := cmd.Flags().GetString("user-id")
		return sendMutation(cmd, mutation{
			Method: http.MethodDelete,
			Path:   client.Path("/api/v1/apps/%s/owners/%s", args[0], userID),
		})
	},
}

func init() {
	appsRemoveOwnerCmd.Flags().String("user-id", "", "C1 user ID to remove as owner")
	markRequired(appsRemoveOwnerCmd, "user-id")
	markMutating(appsRemoveOwnerCmd)
	appsCmd.AddCommand(appsRemoveOwnerCmd)
}
