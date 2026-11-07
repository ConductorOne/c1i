package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var accountsListCmd = &cobra.Command{
	Use:   "list",
	Short: "Search and list accounts (app users) for an application (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id"); err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newListClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
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
		requestedPageSize := pageSizeFlag(cmd)
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		enc := newEmitter(cmd)
		guard := newPaginationCursorGuard()
		route := "POST /api/v1/search/app_users"
		for !limitReached(enc.Written(), limit) {
			if guard.tokenAlreadyUsed(pageToken) {
				return guard.warnStuck(cmd, route)
			}
			pageSize := requestedPageSize
			if !enc.Filtered() {
				pageSize = effectivePageSize(requestedPageSize, limit, enc.Written())
			}
			body := map[string]any{
				"appId":    appID,
				"pageSize": pageSize,
			}
			if pageToken != "" {
				body["pageToken"] = pageToken
			}
			if query != "" {
				body["query"] = query
			}
			if status != "" {
				body["appUserStatuses"] = []string{status}
			}
			if appUserType != "" {
				body["appUserTypes"] = []string{appUserType}
			}

			data, err := c.Post(cmd.Context(), "/api/v1/search/app_users", body)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List []struct {
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
				} `json:"list"`
				NextPageToken string `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			ids := make([]string, len(resp.List))
			for i, it := range resp.List {
				ids[i] = it.AppUser.ID
			}
			keep, err := guard.checkPage(cmd, route, ids)
			if err != nil {
				return err
			}
			resp.List = keepRows(resp.List, keep)
			for _, item := range resp.List {
				a := item.AppUser
				if unmappedOnly && a.IdentityUserID != "" {
					continue
				}
				if err := enc.Encode(map[string]string{
					"id":               a.ID,
					"app_id":           a.AppID,
					"display_name":     a.DisplayName,
					"email":            a.Email,
					"username":         a.Username,
					"identity_user_id": a.IdentityUserID,
					"app_user_type":    a.AppUserType,
					"status":           a.Status.Status,
				}); err != nil {
					return err
				}
				if limitReached(enc.Written(), limit) {
					return nil
				}
			}

			if resp.NextPageToken == "" || manualPaging {
				break
			}
			guard.markTokenUsed(pageToken)
			pageToken = resp.NextPageToken
		}

		return nil
	},
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
