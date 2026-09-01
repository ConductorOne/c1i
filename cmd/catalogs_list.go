package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

// catalogListItem is the subset of the RequestCatalogView surfaced in
// `catalogs list` rows. The catalog itself is nested one level down, under
// "requestCatalog".
//
// The view's memberCount sibling is deliberately not read: this endpoint
// reports it as "0" for every catalog, and takes no parameter that would
// populate it. `catalogs get` reports a non-zero count on the ones checked.
type catalogListItem struct {
	RequestCatalog struct {
		ID                string `json:"id"`
		DisplayName       string `json:"displayName"`
		Description       string `json:"description"`
		Published         bool   `json:"published"`
		VisibleToEveryone bool   `json:"visibleToEveryone"`
		RequestBundle     bool   `json:"requestBundle"`
		DeletedAt         string `json:"deletedAt"`
	} `json:"requestCatalog"`
}

// catalogRow flattens a catalogListItem into the NDJSON output row. The three
// booleans stay bools, so `jq 'select(.published)'` means what it reads as, and
// deleted_at is nil, not "", on a live catalog — see CLAUDE.md's row-fidelity
// convention.
func catalogRow(c catalogListItem) map[string]any {
	return map[string]any{
		"id":                  c.RequestCatalog.ID,
		"display_name":        c.RequestCatalog.DisplayName,
		"description":         c.RequestCatalog.Description,
		"published":           c.RequestCatalog.Published,
		"visible_to_everyone": c.RequestCatalog.VisibleToEveryone,
		"request_bundle":      c.RequestCatalog.RequestBundle,
		"deleted_at":          nilIfEmpty(c.RequestCatalog.DeletedAt),
	}
}

var catalogsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List request catalogs / access profiles (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newListClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		requestedPageSize := pageSizeFlag(cmd)
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		enc := newEmitter(cmd)
		for !limitReached(enc.Written(), limit) {
			pageSize := requestedPageSize
			if !enc.Filtered() {
				pageSize = effectivePageSize(requestedPageSize, limit, enc.Written())
			}
			params := map[string]string{
				"page_size": strconv.Itoa(pageSize),
			}
			if pageToken != "" {
				params["page_token"] = pageToken
			}

			data, err := c.Get(cmd.Context(), "/api/v1/catalogs", params)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List          []catalogListItem `json:"list"`
				NextPageToken string            `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, item := range resp.List {
				_ = enc.Encode(catalogRow(item))
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
	addPaginationFlags(catalogsListCmd)
	catalogsCmd.AddCommand(catalogsListCmd)
}
