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

// userCountNote is quoted verbatim by README.md and cmd/agents.md
// (TestUserCountNoteDocumented).
const userCountNote = "`user_count` is the platform's stored count, not a live tally: on some apps it differs from the number of `accounts list --app-id <app-id>` rows. To size a change, count those rows, not `user_count`."

var appsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List applications (NDJSON output)",
	Long:  "List applications (NDJSON output).\n\n" + userCountNote,
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
		guard := newPaginationCursorGuard()
		route := "GET /api/v1/apps"
		for !limitReached(enc.Written(), limit) {
			if guard.tokenAlreadyUsed(pageToken) {
				return guard.warnStuck(cmd, route)
			}
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

			ids := make([]string, len(resp.List))
			for i, it := range resp.List {
				ids[i] = it.ID
			}
			keep, err := guard.checkPage(cmd, route, ids)
			if err != nil {
				return err
			}
			resp.List = keepRows(resp.List, keep)
			for _, a := range resp.List {
				if err := enc.Encode(appRow(a)); err != nil {
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
	addPaginationFlags(appsListCmd)
	appsCmd.AddCommand(appsListCmd)
}
