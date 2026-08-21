package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// entitlementListItem is one row of the search/entitlements response.
type entitlementListItem struct {
	AppEntitlement struct {
		ID          string    `json:"id"`
		AppID       string    `json:"appId"`
		DisplayName string    `json:"displayName"`
		Description string    `json:"description"`
		Slug        string    `json:"slug"`
		GrantCount  flexInt64 `json:"grantCount"`
		Purpose     string    `json:"purpose"`
		DeletedAt   string    `json:"deletedAt"`
	} `json:"appEntitlement"`
}

// entitlementRow flattens an entitlementListItem into the NDJSON output row.
// grant_count keeps its real numeric type (the API sends it as a JSON
// string) and deleted_at is nil, not "", on a live entitlement.
func entitlementRow(item entitlementListItem) map[string]any {
	e := item.AppEntitlement
	return map[string]any{
		"id":           e.ID,
		"app_id":       e.AppID,
		"display_name": e.DisplayName,
		"description":  e.Description,
		"slug":         e.Slug,
		"grant_count":  int64(e.GrantCount),
		"purpose":      e.Purpose,
		"deleted_at":   nilIfEmpty(e.DeletedAt),
	}
}

var entitlementsListCmd = &cobra.Command{
	Use:   "list",
	Short: "Search and list application entitlements (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newListClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		appID, _ := cmd.Flags().GetString("app-id")
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
				List          []entitlementListItem `json:"list"`
				NextPageToken string                `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, item := range resp.List {
				_ = enc.Encode(entitlementRow(item))
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
	entitlementsListCmd.Flags().String("app-id", "", "Filter by application ID")
	entitlementsListCmd.Flags().String("query", "", "Search entitlement display name")
	entitlementsListCmd.Flags().Int("page-size", 50, "Results per page (max 100)")
	entitlementsListCmd.Flags().String("page-token", "", "Pagination cursor")
	addLimitFlag(entitlementsListCmd)
	entitlementsCmd.AddCommand(entitlementsListCmd)
}
