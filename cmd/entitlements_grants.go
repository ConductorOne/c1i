package cmd

import (
	"encoding/json"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// entitlementGrantItem is one row of SearchAppEntitlementsWithExpired: a grant
// with its account (appUser, flat), the C1 user, and two timestamps.
type entitlementGrantItem struct {
	AppID            string `json:"appId"`
	AppEntitlementID string `json:"appEntitlementId"`
	AppUserID        string `json:"appUserId"`
	AppUser          struct {
		ID             string `json:"id"`
		DisplayName    string `json:"displayName"`
		Email          string `json:"email"`
		Username       string `json:"username"`
		IdentityUserID string `json:"identityUserId"`
		AppUserType    string `json:"appUserType"`
		DeletedAt      string `json:"deletedAt"`
	} `json:"appUser"`
	User struct {
		DisplayName string `json:"displayName"`
		Email       string `json:"email"`
	} `json:"user"`
	Discovered   string            `json:"discovered"`
	Expired      string            `json:"expired"`
	GrantReasons []json.RawMessage `json:"grantReasons"`
	GrantSources []json.RawMessage `json:"grantSources"`
}

func entitlementGrantAppUserID(item entitlementGrantItem) string {
	return firstNonEmpty(item.AppUserID, item.AppUser.ID)
}

// entitlementGrantRow uses grantRow's names for the fields both lists carry.
func entitlementGrantRow(item entitlementGrantItem) map[string]any {
	au := item.AppUser
	return map[string]any{
		"app_id":                nilIfEmpty(item.AppID),
		"entitlement_id":        nilIfEmpty(item.AppEntitlementID),
		"app_user_id":           nilIfEmpty(entitlementGrantAppUserID(item)),
		"app_user_display_name": nilIfEmpty(au.DisplayName),
		"app_user_deleted_at":   nilIfEmpty(au.DeletedAt),
		"app_user_type":         nilIfEmpty(au.AppUserType),
		"email":                 nilIfEmpty(au.Email),
		"username":              nilIfEmpty(au.Username),
		"identity_user_id":      nilIfEmpty(au.IdentityUserID),
		"user_display_name":     nilIfEmpty(item.User.DisplayName),
		"user_email":            nilIfEmpty(item.User.Email),
		"discovered_at":         nilIfEmpty(item.Discovered),
		"expires_at":            nilIfEmpty(item.Expired),
		"grant_source_count":    len(item.GrantSources),
		"grant_reason_count":    len(item.GrantReasons),
	}
}

var entitlementsGrantsCmd = &cobra.Command{
	Use:   "grants <entitlement-id>",
	Short: "List an entitlement's grants with expiry and discovery times (NDJSON output)",
	Long: `List every grant of one app entitlement via GET .../grants, with the
account, the C1 user it maps to, and two times "grants list" does not show:
discovered_at and expires_at (the API's "discovered" and "expired"). In
testing expires_at held the future end time set by "grants update-duration",
and was null for a standing grant and once that time had passed.

grant_source_count is the number of groups/roles the grant is inherited
through (0 = direct), as in "grants list"; grant_reason_count counts the
API's grantReasons.

A well-formed entitlement or app id that does not exist returns zero rows and
exit 0, not a 404 -- the endpoint answers 200 with an empty list. An empty
result here is either "no grants" or "wrong id"; "entitlements get" on the
same ids exits 4 and can tell you which.

Auto-paginates to completion like every other list command; the server
refuses a page size over 100.

Example:
  c1i entitlements grants <entitlement-id> --app-id <app-id>`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		path := client.Path("/api/v1/apps/%s/entitlements/%s/grants", appID, args[0])
		return listPages(cmd, c, pageSpec[entitlementGrantItem]{
			Route: "GET " + path,
			Fetch: getPage(cmd.Context(), path, nil),
			Parse: parseList[entitlementGrantItem],
			ID: func(item entitlementGrantItem) string {
				return scopedRowID(item.AppID, item.AppEntitlementID, entitlementGrantAppUserID(item))
			},
			Rows:      rowFn(entitlementGrantRow),
			Composite: true,
		})
	},
}

func init() {
	entitlementsGrantsCmd.Flags().String("app-id", "", "Application ID the entitlement belongs to")
	markRequired(entitlementsGrantsCmd, "app-id")
	addPaginationFlags(entitlementsGrantsCmd)
	entitlementsCmd.AddCommand(entitlementsGrantsCmd)
}
