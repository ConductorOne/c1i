package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

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
		"id":           sp.ID,
		"display_name": sp.DisplayName,
		"email":        nilIfEmpty(sp.User.Email),
		"created_at":   nilIfEmpty(sp.CreatedAt),
		"updated_at":   nilIfEmpty(sp.UpdatedAt),
	}
}

var servicePrincipalsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List service principals (NDJSON output)",
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
			params := map[string]string{"page_size": strconv.Itoa(pageSize)}
			if pageToken != "" {
				params["page_token"] = pageToken
			}

			data, err := c.Get(cmd.Context(), "/api/v1/service_principals", params)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List          []spListItem `json:"list"`
				NextPageToken string       `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, sp := range resp.List {
				_ = enc.Encode(spRow(sp))
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
	addPaginationFlags(servicePrincipalsListCmd)
	servicePrincipalsCmd.AddCommand(servicePrincipalsListCmd)
}
