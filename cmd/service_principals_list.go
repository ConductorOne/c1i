package cmd

import (
	"github.com/spf13/cobra"
)

// spListItem is the subset of ServicePrincipal surfaced in list rows.
type spListItem struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
	User        struct {
		Email string `json:"email"`
	} `json:"user"`
}

// spRow flattens a service principal into an NDJSON output row. Optional
// timestamps and email emit null, not "", per CLAUDE.md's row-fidelity rule.
func spRow(sp spListItem) map[string]any {
	return map[string]any{
		"id":           nilIfEmpty(sp.ID),
		"display_name": nilIfEmpty(sp.DisplayName),
		"email":        nilIfEmpty(sp.User.Email),
		"created_at":   nilIfEmpty(sp.CreatedAt),
		"updated_at":   nilIfEmpty(sp.UpdatedAt),
	}
}

var servicePrincipalsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List service principals (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		return listPages(cmd, c, pageSpec[spListItem]{
			Route: "GET /api/v1/service_principals",
			Fetch: getPage(cmd.Context(), "/api/v1/service_principals", nil),
			Parse: parseList[spListItem],
			ID:    func(it spListItem) string { return it.ID },
			Rows:  rowFn(spRow),
		})
	},
}

func init() {
	addPaginationFlags(servicePrincipalsListCmd)
	servicePrincipalsCmd.AddCommand(servicePrincipalsListCmd)
}
