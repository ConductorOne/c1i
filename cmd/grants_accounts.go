package cmd

import (
	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var grantsAccountsCmd = &cobra.Command{
	Use:   "accounts",
	Short: "List which of a C1 user's accounts in an app hold an entitlement (NDJSON output)",
	Long: `List a C1 user's grants of one entitlement, one row per app account (app
user) via GET .../entitlements/{entitlement-id}/users/{user-id}/grants. It
answers "which of this person's accounts in the app hold this entitlement",
which matters when a user has more than one account there.

Rows: app_id, entitlement_id, app_user_id, created_at, deprovision_at
(null for a standing grant), deleted_at, and grant_source_count (0 = direct
grant, as in "grants list").

A well-formed user, entitlement or app id that does not exist returns zero
rows and exit 0, not a 404 -- the endpoint answers 200 with an empty list.
"users get" and "entitlements get" exit 4 for an id that does not exist.

The endpoint does not paginate; --limit caps the rows printed.

Example:
  c1i grants accounts --app-id <app-id> --entitlement-id <entitlement-id> --user-id <user-id>`,
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		entitlementID, _ := cmd.Flags().GetString("entitlement-id")
		userID, _ := cmd.Flags().GetString("user-id")
		path := client.Path("/api/v1/apps/%s/entitlements/%s/users/%s/grants", appID, entitlementID, userID)
		return getFixedListKey(cmd, path, "bindings", grantBindingRow)
	},
}

// grantBindingRow flattens an AppEntitlementUserBinding.
func grantBindingRow(b map[string]any) map[string]any {
	str := func(k string) any { s, _ := b[k].(string); return nilIfEmpty(s) }
	sources, _ := b["grantSources"].([]any)
	return map[string]any{
		"app_id":             str("appId"),
		"entitlement_id":     str("appEntitlementId"),
		"app_user_id":        str("appUserId"),
		"created_at":         str("createdAt"),
		"deprovision_at":     str("deprovisionAt"),
		"deleted_at":         str("deletedAt"),
		"grant_source_count": len(sources),
	}
}

func init() {
	f := grantsAccountsCmd.Flags()
	f.String("app-id", "", "Application ID the entitlement belongs to")
	f.String("entitlement-id", "", "Entitlement ID")
	f.String("user-id", "", "C1 user ID whose accounts to list")
	markRequired(grantsAccountsCmd, "app-id", "entitlement-id", "user-id")
	addLimitFlag(grantsAccountsCmd)
	grantsCmd.AddCommand(grantsAccountsCmd)
}
