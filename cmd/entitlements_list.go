package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var entitlementsListCmd = &cobra.Command{
	Use:   "list",
	Short: "Search and list application entitlements (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		appID, _ := cmd.Flags().GetString("app-id")
		query, _ := cmd.Flags().GetString("query")
		requestedPageSize := clampPageSize(getIntFlag(cmd, "page-size"))
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		enc := newEmitter(cmd.OutOrStdout())
		emitted := 0
		for !limitReached(emitted, limit) {
			pageSize := effectivePageSize(requestedPageSize, limit, emitted)
			body := map[string]any{
				"pageSize": pageSize,
			}
			if pageToken != "" {
				body["pageToken"] = pageToken
			}
			if appID != "" {
				body["appIds"] = []string{appID}
			}
			if query != "" {
				body["query"] = query
			}

			data, err := c.Post(cmd.Context(), "/api/v1/search/entitlements", body)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List []struct {
					AppEntitlement struct {
						ID          string `json:"id"`
						AppID       string `json:"appId"`
						DisplayName string `json:"displayName"`
						Description string `json:"description"`
						Slug        string `json:"slug"`
						GrantCount  string `json:"grantCount"`
						Purpose     string `json:"purpose"`
					} `json:"appEntitlement"`
				} `json:"list"`
				NextPageToken string `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, item := range resp.List {
				e := item.AppEntitlement
				_ = enc.Encode(map[string]string{
					"id":           e.ID,
					"app_id":       e.AppID,
					"display_name": e.DisplayName,
					"description":  e.Description,
					"slug":         e.Slug,
					"grant_count":  e.GrantCount,
					"purpose":      e.Purpose,
				})
				emitted++
				if limitReached(emitted, limit) {
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
	entitlementsListCmd.Flags().String("app-id", "", "Filter by application ID")
	entitlementsListCmd.Flags().String("query", "", "Search entitlement display name")
	entitlementsListCmd.Flags().Int("page-size", 50, "Results per page (max 100)")
	entitlementsListCmd.Flags().String("page-token", "", "Pagination cursor")
	addLimitFlag(entitlementsListCmd)
	entitlementsCmd.AddCommand(entitlementsListCmd)
}
