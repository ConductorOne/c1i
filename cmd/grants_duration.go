package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// grantDurationPath is a grant's update/remove-grant-duration endpoint; a
// grant is addressed by its app, entitlement and account.
func grantDurationPath(cmd *cobra.Command, verb string) string {
	appID, _ := cmd.Flags().GetString("app-id")
	entitlementID, _ := cmd.Flags().GetString("entitlement-id")
	appUserID, _ := cmd.Flags().GetString("app-user-id")
	return client.Path("/api/v1/apps/%s/entitlements/%s/users/%s/"+verb, appID, entitlementID, appUserID)
}

const grantDurationOutput = `Prints the grant as pretty JSON (the response's binding): .deprovisionAt
is the new end time. The change shows as deprovision_at in "c1i grants list"
and as expires_at in "c1i entitlements grants".

An unknown app, entitlement or account, or an account without this grant,
exits 4 from the server. --app-user-id is the app_user_id column of
"grants list".`

var grantsUpdateDurationCmd = &cobra.Command{
	Use:   "update-duration",
	Short: "Set when an existing grant is automatically revoked (pretty JSON)",
	Long: `Set the end time of an existing grant via POST .../users/{app-user-id}/
update-grant-duration.

At that time C1 starts the revoke. In testing on a manually managed
entitlement it opened a revoke task ("auto created for deprovision") and the
grant stayed listed, deprovision_at reading null, while that task was open.

--deprovision-at is an RFC3339 timestamp (2026-12-01T00:00:00Z); anything
else exits 2 before sending. The server refuses a time in the past with a 400
(exit 2): "new_deprovision_at must be in the future".

To change the duration of a grant still being requested, use
"c1i tasks update-grant-duration"; to make a grant standing again, use
"c1i grants remove-duration".

` + grantDurationOutput + `

Example:
  c1i grants update-duration --app-id <app-id> --entitlement-id <entitlement-id> \
    --app-user-id <app-user-id> --deprovision-at 2026-12-01T00:00:00Z`,
	RunE: func(cmd *cobra.Command, args []string) error {
		at, _ := cmd.Flags().GetString("deprovision-at")
		if err := validateRFC3339("--deprovision-at", at); err != nil {
			return err
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: grantDurationPath(cmd, "update-grant-duration"),
			Body: map[string]any{"newDeprovisionAt": at}, Unwrap: named("binding")})
	},
}

var grantsRemoveDurationCmd = &cobra.Command{
	Use:   "remove-duration",
	Short: "Make an existing grant standing (no automatic revocation) (pretty JSON)",
	Long: `Remove the end time of an existing grant via POST .../users/{app-user-id}/
remove-grant-duration, so it is no longer revoked automatically; the printed
.deprovisionAt is then null.

` + grantDurationOutput + `

Example:
  c1i grants remove-duration --app-id <app-id> --entitlement-id <entitlement-id> --app-user-id <app-user-id>`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: grantDurationPath(cmd, "remove-grant-duration"),
			Body: map[string]any{}, Unwrap: named("binding")})
	},
}

func init() {
	for _, c := range []*cobra.Command{grantsUpdateDurationCmd, grantsRemoveDurationCmd} {
		f := c.Flags()
		f.String("app-id", "", "Application ID the entitlement belongs to")
		f.String("entitlement-id", "", "Entitlement ID of the grant")
		f.String("app-user-id", "", "App account (app user) ID holding the grant; see the app_user_id column of \"grants list\"")
		markRequired(c, "app-id", "entitlement-id", "app-user-id")
		markMutating(c)
		grantsCmd.AddCommand(c)
	}
	grantsUpdateDurationCmd.Flags().String("deprovision-at", "", "When to revoke the grant, as an RFC3339 timestamp (e.g. 2026-12-01T00:00:00Z); must be in the future")
	markRequired(grantsUpdateDurationCmd, "deprovision-at")
}
