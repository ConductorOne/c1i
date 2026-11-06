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

Reads POST /api/v1/search/users, which with no --origin returns only
directory-origin users: local-origin users (service principals among them)
and system users are excluded. Pass --origin all to list every user, or
repeat --origin with directory, local, system, or a USER_ORIGIN_* value (as a
row's origin prints) to pick origins.`,
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
		origins, err := userOrigins(cmd)
		if err != nil {
			return err
		}

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
			if len(origins) > 0 {
				body["origins"] = origins
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
						Origin      string `json:"origin"`
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
				row := map[string]string{
					"id":           u.ID,
					"display_name": u.DisplayName,
					"email":        u.Email,
					"department":   u.Department,
					"job_title":    u.JobTitle,
					"status":       u.Status,
				}
				if u.Origin != "" {
					row["origin"] = u.Origin
				}
				if err := enc.Encode(row); err != nil {
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
	addRepeatableStringFlag(usersListCmd, "origin", "Filter by origin: directory, local, system, all, or a USER_ORIGIN_* value (repeatable; default: the server's, directory only)")
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

// userOriginValues maps --origin values to the search API's enum, in the
// order "all" expands to.
var userOriginValues = []struct{ flag, api string }{
	{"directory", "USER_ORIGIN_DIRECTORY"},
	{"local", "USER_ORIGIN_LOCAL"},
	{"system", "USER_ORIGIN_SYSTEM"},
}

// userOrigins reads --origin (case-insensitive) into the API enum, deduped and
// in a stable order; nil when unset, so the server's default applies. A
// USER_ORIGIN_* name passes through as-is, so a row's origin value round-trips
// and an enum value newer than c1i reaches the server.
func userOrigins(cmd *cobra.Command) ([]string, error) {
	values, err := repeatableStringFlag(cmd, "origin")
	if err != nil || len(values) == 0 {
		return nil, err
	}
	want := map[string]bool{}
	var extra []string
	for _, raw := range values {
		v := strings.ToLower(strings.TrimSpace(raw))
		if strings.HasPrefix(v, "user_origin_") {
			if api := strings.ToUpper(v); !want[api] {
				want[api] = true
				extra = append(extra, api)
			}
			continue
		}
		known := v == "all"
		for _, o := range userOriginValues {
			if v == "all" || v == o.flag {
				want[o.api] = true
				known = true
			}
		}
		if !known {
			return nil, &usageError{fmt.Errorf("invalid --origin %q: want directory, local, system, all, or a USER_ORIGIN_* value", raw)}
		}
	}
	var out []string
	for _, o := range userOriginValues {
		if want[o.api] {
			out = append(out, o.api)
			delete(want, o.api)
		}
	}
	for _, api := range extra {
		if want[api] {
			out = append(out, api)
		}
	}
	return out, nil
}
