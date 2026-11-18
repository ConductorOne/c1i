package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// notSignInEntitlementMsg is the server's refusal for an entitlement
// remove-membership does not handle, quoted so the help greps back to it.
const notSignInEntitlementMsg = "entitlement is not an SSO application sign-in entitlement"

var grantsRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove one account's membership of an entitlement directly",
	Long: `Remove one app account (app user) from an entitlement via DELETE
.../entitlements/{entitlement-id}/remove-membership.

Per the API's description it covers a manually managed entitlement, C1
catalogs, groups and profile types, and an SSO application's sign-in
entitlement (removing only direct manual access), and an access profile only
on a tenant with the JML feature. In testing, the server refused a manually
managed entitlement of the custom and group resource types, an account
granted with "grants add", with a 400 (exit 2):
  ` + notSignInEntitlementMsg + `
The policy-respecting removal is "c1i requests create revoke".

An unknown entitlement or app exits 4 (the server answers 404). The server
checks the entitlement before the account: on a refused entitlement an
unknown --app-user-id gets the same 400. --app-user-id is the app_user_id
column of "grants list".

Prints the API's response, {"revokeTaskId": ...}: the removal runs through
that revoke task, so the grant may stay visible until the task completes;
follow it with "c1i tasks get <task-id>".

Example:
  c1i grants remove --app-id <app-id> --entitlement-id <entitlement-id> --app-user-id <app-user-id>`,
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		entitlementID, _ := cmd.Flags().GetString("entitlement-id")
		appUserID, _ := cmd.Flags().GetString("app-user-id")
		path := client.Path("/api/v1/apps/%s/entitlements/%s/remove-membership", appID, entitlementID)
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: path, Body: map[string]any{"appUserId": appUserID}})
	},
}

func init() {
	f := grantsRemoveCmd.Flags()
	f.String("app-id", "", "Application ID the entitlement belongs to")
	f.String("entitlement-id", "", "Entitlement ID to remove the account from")
	f.String("app-user-id", "", "App account (app user) ID to remove; see the app_user_id column of \"grants list\"")
	markRequired(grantsRemoveCmd, "app-id", "entitlement-id", "app-user-id")
	markMutating(grantsRemoveCmd)
	grantsCmd.AddCommand(grantsRemoveCmd)
}
