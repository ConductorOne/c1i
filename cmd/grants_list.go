package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// grantListItem is one row of the SearchGrants response
// (AppEntitlementWithUserBinding): a grant view (account + timestamps + grant
// sources) plus the entitlement it binds to.
type grantListItem struct {
	AppEntitlementUserBinding struct {
		CreatedAt     string `json:"appEntitlementUserBindingCreatedAt"`
		DeprovisionAt string `json:"appEntitlementUserBindingDeprovisionAt"`
		AppUser       struct {
			AppUser struct {
				ID             string `json:"id"`
				AppID          string `json:"appId"`
				DisplayName    string `json:"displayName"`
				Email          string `json:"email"`
				Username       string `json:"username"`
				IdentityUserID string `json:"identityUserId"`
				AppUserType    string `json:"appUserType"`
				DeletedAt      string `json:"deletedAt"`
			} `json:"appUser"`
		} `json:"appUser"`
		// grantSources lists the groups/roles a grant is inherited through; an
		// empty list means the grant is direct.
		GrantSources []json.RawMessage `json:"grantSources"`
	} `json:"appEntitlementUserBinding"`
	Entitlement struct {
		AppEntitlement struct {
			ID          string `json:"id"`
			AppID       string `json:"appId"`
			DisplayName string `json:"displayName"`
			Slug        string `json:"slug"`
			DeletedAt   string `json:"deletedAt"`
		} `json:"appEntitlement"`
	} `json:"entitlement"`
}

// grantRow flattens a grant into the NDJSON output row.
func grantRow(item grantListItem) map[string]any {
	b := item.AppEntitlementUserBinding
	e := item.Entitlement.AppEntitlement
	au := b.AppUser.AppUser
	// The entitlement view carries the app id; fall back to the account's app id
	// if the entitlement wasn't expanded for some reason.
	appID := e.AppID
	if appID == "" {
		appID = au.AppID
	}
	return map[string]any{
		"app_id":                   appID,
		"entitlement_id":           e.ID,
		"entitlement_display_name": e.DisplayName,
		"entitlement_slug":         e.Slug,
		// A grant to a soft-deleted entitlement or account is still an active
		// binding server-side (only the entitlement/account itself, not this
		// grant, was deleted) — surface both so a stale grant is visible.
		"entitlement_deleted_at": nilIfEmpty(e.DeletedAt),
		"app_user_id":            au.ID,
		"app_user_display_name":  au.DisplayName,
		"app_user_deleted_at":    nilIfEmpty(au.DeletedAt),
		"email":                  au.Email,
		"username":               au.Username,
		"identity_user_id":       au.IdentityUserID,
		"app_user_type":          au.AppUserType,
		"created_at":             b.CreatedAt,
		"deprovision_at":         nilIfEmpty(b.DeprovisionAt),
		// 0 = direct grant; >0 = inherited through that many groups/roles.
		"grant_source_count": len(b.GrantSources),
	}
}

var grantsListCmd = &cobra.Command{
	Use:   "list",
	Short: "Search and list access grants (NDJSON output)",
	Long: `Search and list grants. At least one filter is required.

  # Who holds an entitlement?
  c1i grants list --app-id APP --entitlement-id ENT

  # What does a C1 identity have across apps?
  c1i grants list --user-id USER

  # Every grant in an app
  c1i grants list --app-id APP`,
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		userID, _ := cmd.Flags().GetString("user-id")
		appUserID, _ := cmd.Flags().GetString("app-user-id")
		entitlementID, _ := cmd.Flags().GetString("entitlement-id")

		if appID == "" && userID == "" && appUserID == "" && entitlementID == "" {
			return &usageError{fmt.Errorf("at least one filter is required: --app-id, --user-id, --app-user-id, or --entitlement-id")}
		}
		if entitlementID != "" && appID == "" {
			return &usageError{fmt.Errorf("--entitlement-id requires --app-id (entitlements are scoped to an app)")}
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newListClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		requestedPageSize := clampPageSize(getIntFlag(cmd, "page-size"))
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		enc := newEmitter(cmd)
		for !limitReached(enc.Written(), limit) {
			pageSize := effectivePageSize(requestedPageSize, limit, enc.Written())
			body := map[string]any{
				"pageSize": pageSize,
			}
			if pageToken != "" {
				body["pageToken"] = pageToken
			}
			if appID != "" {
				body["appIds"] = []string{appID}
			}
			if userID != "" {
				body["userId"] = userID
			}
			if appUserID != "" {
				body["appUserIds"] = []string{appUserID}
			}
			if entitlementID != "" {
				body["entitlementRefs"] = []map[string]string{{"appId": appID, "id": entitlementID}}
			}

			data, err := c.Post(cmd.Context(), "/api/v1/search/grants", body)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List          []grantListItem `json:"list"`
				NextPageToken string          `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, item := range resp.List {
				_ = enc.Encode(grantRow(item))
				if limitReached(enc.Written(), limit) {
					return nil
				}
			}

			if resp.NextPageToken == "" || manualPaging {
				break
			}
			pageToken = resp.NextPageToken
		}

		return nil
	},
}

func init() {
	grantsListCmd.Flags().String("app-id", "", "Filter to grants in this application")
	grantsListCmd.Flags().String("user-id", "", "Filter to grants held by this C1 identity user")
	grantsListCmd.Flags().String("app-user-id", "", "Filter to grants held by this app account (app user)")
	grantsListCmd.Flags().String("entitlement-id", "", "Filter to grants of this entitlement (requires --app-id)")
	grantsListCmd.Flags().Int("page-size", 50, "Results per page (max 100)")
	grantsListCmd.Flags().String("page-token", "", "Pagination cursor")
	addLimitFlag(grantsListCmd)
	grantsCmd.AddCommand(grantsListCmd)
}
