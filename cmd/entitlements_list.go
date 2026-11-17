package cmd

import (
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
		"id":           nilIfEmpty(e.ID),
		"app_id":       nilIfEmpty(e.AppID),
		"display_name": nilIfEmpty(e.DisplayName),
		"description":  nilIfEmpty(e.Description),
		"slug":         nilIfEmpty(e.Slug),
		"grant_count":  int64(e.GrantCount),
		"purpose":      nilIfEmpty(e.Purpose),
		"deleted_at":   nilIfEmpty(e.DeletedAt),
	}
}

var entitlementsListCmd = &cobra.Command{
	Use:   "list",
	Short: "Search and list application entitlements (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		appID, _ := cmd.Flags().GetString("app-id")
		query, _ := cmd.Flags().GetString("query")
		body := map[string]any{}
		if appID != "" {
			body["appIds"] = []string{appID}
		}
		if query != "" {
			body["query"] = query
		}
		return listPages(cmd, c, pageSpec[entitlementListItem]{
			Route:     "POST /api/v1/search/entitlements",
			Fetch:     postPage(cmd.Context(), "/api/v1/search/entitlements", body),
			Parse:     parseList[entitlementListItem],
			ID:        entitlementGuardID,
			Rows:      rowFn(entitlementRow),
			Composite: true,
		})
	},
}

// entitlementGuardID keys an entitlement on app and id: an id repeats across apps.
func entitlementGuardID(it entitlementListItem) string {
	return scopedRowID(it.AppEntitlement.AppID, it.AppEntitlement.ID)
}

func init() {
	entitlementsListCmd.Flags().String("app-id", "", "Filter by application ID")
	entitlementsListCmd.Flags().String("query", "", "Search entitlement display name")
	addPaginationFlags(entitlementsListCmd)
	entitlementsCmd.AddCommand(entitlementsListCmd)
}
