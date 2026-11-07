package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var accountsSetOwnerCmd = &cobra.Command{
	Use:   "set-owner <app-user-id>",
	Short: "Set the identity user (owner) for an app account",
	Long: `Set the C1 identity user that owns an app account (app user).

Prints the app user as pretty JSON, unwrapped like the other reads: the id is
.id and the owner is .identityUserId. --fields is a usage error on a
mutation. Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		userID, _ := cmd.Flags().GetString("user-id")

		path := client.Path("/api/v1/apps/%s/app_users/%s", appID, args[0])
		body := map[string]any{
			"appUser": map[string]any{
				"identityUserId": userID,
			},
			"updateMask": "identityUserId",
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: body})
	},
}

func init() {
	accountsSetOwnerCmd.Flags().String("app-id", "", "Application ID")
	accountsSetOwnerCmd.Flags().String("user-id", "", "C1 user ID to set as owner")
	markRequired(accountsSetOwnerCmd, "app-id")
	markRequired(accountsSetOwnerCmd, "user-id")
	markMutating(accountsSetOwnerCmd)
	accountsCmd.AddCommand(accountsSetOwnerCmd)
}
