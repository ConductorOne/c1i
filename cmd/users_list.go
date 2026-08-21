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

			for _, item := range resp.List {
				u := item.User
				_ = enc.Encode(map[string]string{
					"id":           u.ID,
					"display_name": u.DisplayName,
					"email":        u.Email,
					"department":   u.Department,
					"job_title":    u.JobTitle,
					"status":       u.Status,
				})
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
	usersListCmd.Flags().String("query", "", "Fuzzy search on name or email")
	usersListCmd.Flags().String("email", "", "Exact email match")
	usersListCmd.Flags().String("status", "", "Filter by status: enabled, disabled, deleted")
	usersListCmd.Flags().Int("page-size", 50, "Number of results per page (max 100)")
	usersListCmd.Flags().String("page-token", "", "Pagination cursor for next page")
	addLimitFlag(usersListCmd)
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
