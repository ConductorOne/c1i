package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var usersListCmd = &cobra.Command{
	Use:   "list",
	Short: "Search and list C1 users (NDJSON output)",
	Long: `Search and list C1 users (NDJSON output).

Reads POST /api/v1/search/users, which with no origins filter returns only
directory-origin users: local-origin (service) users and system users are
excluded. The search's origins filter includes them:

  c1i api --method POST --path /api/v1/search/users --paginate \
    --body '{"origins":["USER_ORIGIN_DIRECTORY","USER_ORIGIN_LOCAL","USER_ORIGIN_SYSTEM"]}'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newListClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		query, _ := cmd.Flags().GetString("query")
		email, _ := cmd.Flags().GetString("email")
		status, _ := cmd.Flags().GetString("status")
		requestedPageSize := pageSizeFlag(cmd)
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		enc := newEmitter(cmd)
		guard := newPaginationCursorGuard()
		route := "POST /api/v1/search/users"
		for !limitReached(enc.Written(), limit) {
			if guard.tokenAlreadyUsed(pageToken) {
				return guard.warnStuck(cmd, route)
			}
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
			if query != "" {
				body["query"] = query
			}
			if email != "" {
				body["email"] = email
			}
			if status != "" {
				body["userStatuses"] = []string{mapUserStatus(status)}
			}

			data, err := c.Post(cmd.Context(), "/api/v1/search/users", body)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List []struct {
					User struct {
						ID          string `json:"id"`
						DisplayName string `json:"displayName"`
						Email       string `json:"email"`
						Department  string `json:"department"`
						JobTitle    string `json:"jobTitle"`
						Status      string `json:"status"`
					} `json:"user"`
				} `json:"list"`
				NextPageToken string `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			ids := make([]string, len(resp.List))
			for i, it := range resp.List {
				ids[i] = it.User.ID
			}
			keep, err := guard.checkPage(cmd, route, ids)
			if err != nil {
				return err
			}
			resp.List = keepRows(resp.List, keep)
			for _, item := range resp.List {
				u := item.User
				if err := enc.Encode(map[string]string{
					"id":           u.ID,
					"display_name": u.DisplayName,
					"email":        u.Email,
					"department":   u.Department,
					"job_title":    u.JobTitle,
					"status":       u.Status,
				}); err != nil {
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
	usersListCmd.Flags().String("query", "", "Fuzzy search on name or email")
	usersListCmd.Flags().String("email", "", "Exact email match")
	usersListCmd.Flags().String("status", "", "Filter by status: enabled, disabled, deleted")
	addPaginationFlags(usersListCmd)
	usersCmd.AddCommand(usersListCmd)
}

// mapUserStatus translates the user-friendly --status value to the
// enum the search/users API accepts. The API enum values are bare
// (ENABLED / DISABLED / DELETED), not prefixed. Input is case-insensitive
// so `--status enabled` and `--status ENABLED` both work.
func mapUserStatus(s string) string {
	switch strings.ToLower(s) {
	case "enabled":
		return "ENABLED"
	case "disabled":
		return "DISABLED"
	case "deleted":
		return "DELETED"
	default:
		return s
	}
}
