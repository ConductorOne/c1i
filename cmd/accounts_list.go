package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

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
		status, _ := cmd.Flags().GetString("status")
		appUserType, _ := cmd.Flags().GetString("type")
		unmappedOnly, _ := cmd.Flags().GetBool("unmapped-only")
		query, _ := cmd.Flags().GetString("query")
		requestedPageSize := clampPageSize(getIntFlag(cmd, "page-size"))
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		enc := newEmitter(cmd)
		for !limitReached(enc.Written(), limit) {
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
				body["appUserStatuses"] = []string{mapAppUserStatus(status)}
			}
			if appUserType != "" {
				body["appUserTypes"] = []string{mapAppUserType(appUserType)}
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

			for _, item := range resp.List {
				a := item.AppUser
				if unmappedOnly && a.IdentityUserID != "" {
					continue
				}
				_ = enc.Encode(map[string]string{
					"id":               a.ID,
					"app_id":           a.AppID,
					"display_name":     a.DisplayName,
					"email":            a.Email,
					"username":         a.Username,
					"identity_user_id": a.IdentityUserID,
					"app_user_type":    a.AppUserType,
					"status":           a.Status.Status,
				})
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
	accountsListCmd.Flags().String("app-id", "", "Application ID")
	accountsListCmd.Flags().String("status", "", "Filter: enabled, disabled, deleted")
	accountsListCmd.Flags().String("type", "", "Filter: user, service_account, system_account")
	accountsListCmd.Flags().Bool("unmapped-only", false, "Only show accounts with no linked identity user")
	accountsListCmd.Flags().String("query", "", "Fuzzy search on display name")
	accountsListCmd.Flags().Int("page-size", 50, "Results per page (max 100)")
	accountsListCmd.Flags().String("page-token", "", "Pagination cursor")
	markRequired(accountsListCmd, "app-id")
	addLimitFlag(accountsListCmd)
	accountsCmd.AddCommand(accountsListCmd)
}

// mapAppUserStatus is case-insensitive; both `--status enabled` and `ENABLED` work.
func mapAppUserStatus(s string) string {
	switch strings.ToLower(s) {
	case "enabled":
		return "STATUS_ENABLED"
	case "disabled":
		return "STATUS_DISABLED"
	case "deleted":
		return "STATUS_DELETED"
	default:
		return s
	}
}

// mapAppUserType is case-insensitive.
func mapAppUserType(s string) string {
	switch strings.ToLower(s) {
	case "user":
		return "APP_USER_TYPE_USER"
	case "service_account":
		return "APP_USER_TYPE_SERVICE_ACCOUNT"
	case "system_account":
		return "APP_USER_TYPE_SYSTEM_ACCOUNT"
	default:
		return s
	}
}
