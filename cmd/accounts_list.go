package cmd

import (
	"github.com/spf13/cobra"
)

var accountsListCmd = &cobra.Command{
	Use:   "list",
	Short: "Search and list accounts (app users) for an application (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {

		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		appID, _ := cmd.Flags().GetString("app-id")
		status, err := appUserStatusFlag.parse(mustGetString(cmd, "status"))
		if err != nil {
			return err
		}
		appUserType, err := appUserTypeFlag.parse(mustGetString(cmd, "type"))
		if err != nil {
			return err
		}
		unmappedOnly, _ := cmd.Flags().GetBool("unmapped-only")
		query, _ := cmd.Flags().GetString("query")
		body := map[string]any{"appId": appID}
		if query != "" {
			body["query"] = query
		}
		if status != "" {
			body["appUserStatuses"] = []string{status}
		}
		if appUserType != "" {
			body["appUserTypes"] = []string{appUserType}
		}
		return listPages(cmd, c, pageSpec[appUserSearchItem]{
			Route: "POST /api/v1/search/app_users",
			Fetch: postPage(cmd.Context(), "/api/v1/search/app_users", body),
			Parse: parseList[appUserSearchItem],
			ID:    func(it appUserSearchItem) string { return it.AppUser.ID },
			// --unmapped-only filters client-side, so a fetched row is not
			// necessarily written: don't tighten the page toward --limit.
			FixedSize: unmappedOnly,
			Rows: func(it appUserSearchItem) []any {
				a := it.AppUser
				if unmappedOnly && a.IdentityUserID != "" {
					return nil
				}
				return []any{map[string]any{
					"id":               nilIfEmpty(a.ID),
					"app_id":           nilIfEmpty(a.AppID),
					"display_name":     nilIfEmpty(a.DisplayName),
					"email":            nilIfEmpty(a.Email),
					"username":         nilIfEmpty(a.Username),
					"identity_user_id": nilIfEmpty(a.IdentityUserID),
					"app_user_type":    nilIfEmpty(a.AppUserType),
					"status":           nilIfEmpty(a.Status.Status),
				}}
			},
		})
	},
}

// appUserSearchItem is one row of the search/app_users response.
type appUserSearchItem struct {
	AppUser struct {
		ID             string `json:"id"`
		AppID          string `json:"appId"`
		DisplayName    string `json:"displayName"`
		Email          string `json:"email"`
		Username       string `json:"username"`
		IdentityUserID string `json:"identityUserId"`
		AppUserType    string `json:"appUserType"`
		Status         struct {
			Status string `json:"status"`
		} `json:"status"`
	} `json:"appUser"`
}

func init() {
	accountsListCmd.Flags().String("app-id", "", "Application ID")
	accountsListCmd.Flags().String("status", "", "Filter: "+appUserStatusFlag.usage())
	accountsListCmd.Flags().String("type", "", "Filter: "+appUserTypeFlag.usage())
	accountsListCmd.Flags().Bool("unmapped-only", false, "Only show accounts with no linked identity user")
	accountsListCmd.Flags().String("query", "", "Fuzzy search on display name")
	addPaginationFlags(accountsListCmd)
	markRequired(accountsListCmd, "app-id")
	accountsCmd.AddCommand(accountsListCmd)
}

var appUserStatusFlag = enumFlag{flag: "status", prefix: "STATUS_", unknown: enumPassPrefixed, values: []enumValue{
	{name: "enabled", api: "STATUS_ENABLED"},
	{name: "disabled", api: "STATUS_DISABLED"},
	{name: "deleted", api: "STATUS_DELETED"},
}}

var appUserTypeFlag = enumFlag{flag: "type", prefix: "APP_USER_TYPE_", unknown: enumPassPrefixed, values: []enumValue{
	{name: "user", api: "APP_USER_TYPE_USER"},
	{name: "service-account", api: "APP_USER_TYPE_SERVICE_ACCOUNT"},
	{name: "system-account", api: "APP_USER_TYPE_SYSTEM_ACCOUNT"},
}}
