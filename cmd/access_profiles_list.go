package cmd

import (
	"github.com/spf13/cobra"
)

// catalogListItem is the subset of the RequestCatalogView surfaced in
// `access-profiles list` rows. The catalog itself is nested one level down, under
// "requestCatalog".
//
// The view's memberCount sibling is deliberately not read: this endpoint
// reports it as "0" for every catalog, and takes no parameter that would
// populate it. `access-profiles get` reports a non-zero count on the ones checked.
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
		"id":                  nilIfEmpty(c.RequestCatalog.ID),
		"display_name":        nilIfEmpty(c.RequestCatalog.DisplayName),
		"description":         nilIfEmpty(c.RequestCatalog.Description),
		"published":           c.RequestCatalog.Published,
		"visible_to_everyone": c.RequestCatalog.VisibleToEveryone,
		"request_bundle":      c.RequestCatalog.RequestBundle,
		"deleted_at":          nilIfEmpty(c.RequestCatalog.DeletedAt),
	}
}

var accessProfilesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List access profiles (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		return listPages(cmd, c, pageSpec[catalogListItem]{
			Route: "GET /api/v1/catalogs",
			Fetch: getPage(cmd.Context(), "/api/v1/catalogs", nil),
			Parse: parseList[catalogListItem],
			ID:    func(it catalogListItem) string { return it.RequestCatalog.ID },
			Rows:  rowFn(catalogRow),
		})
	},
}

func init() {
	addPaginationFlags(accessProfilesListCmd)
	accessProfilesCmd.AddCommand(accessProfilesListCmd)
}
