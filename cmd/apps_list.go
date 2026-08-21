package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

// appListItem is the subset of the App message surfaced in `apps list` rows.
type appListItem struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"displayName"`
	Description string    `json:"description"`
	UserCount   flexInt64 `json:"userCount"`
	IconURL     string    `json:"iconUrl"`
	DeletedAt   string    `json:"deletedAt"`
}

// appRow flattens an appListItem into the NDJSON output row. user_count keeps
// its real numeric type (the API sends it as a JSON string) and deleted_at is
// nil, not "", on a live app — see CLAUDE.md's row-fidelity convention.
func appRow(a appListItem) map[string]any {
	return map[string]any{
		"id":           a.ID,
		"display_name": a.DisplayName,
		"description":  a.Description,
		"user_count":   int64(a.UserCount),
		"deleted_at":   nilIfEmpty(a.DeletedAt),
	}
}

var appsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List applications (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		requestedPageSize := clampPageSize(getIntFlag(cmd, "page-size"))
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		enc := newEmitter(cmd)
		emitted := 0
		for !limitReached(emitted, limit) {
			pageSize := effectivePageSize(requestedPageSize, limit, emitted)
			params := map[string]string{
				"page_size": strconv.Itoa(pageSize),
			}
			if pageToken != "" {
				params["page_token"] = pageToken
			}

			data, err := c.Get(cmd.Context(), "/api/v1/apps", params)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List          []appListItem `json:"list"`
				NextPageToken string        `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, a := range resp.List {
				_ = enc.Encode(appRow(a))
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
	appsListCmd.Flags().Int("page-size", 50, "Results per page (max 100)")
	appsListCmd.Flags().String("page-token", "", "Pagination cursor")
	addLimitFlag(appsListCmd)
	appsCmd.AddCommand(appsListCmd)
}
