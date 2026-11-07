package cmd

import (
	"encoding/json"
	"fmt"

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
		status, err := userStatusFlag.parse(mustGetString(cmd, "status"))
		if err != nil {
			return err
		}
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
				body["userStatuses"] = []string{status}
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
	usersListCmd.Flags().String("status", "", "Filter by status: "+userStatusFlag.usage())
	addRepeatableStringFlag(usersListCmd, "origin", "Filter by origin: "+userOriginFlag.usage()+" (repeatable; default: the server's, directory only)")
	addPaginationFlags(usersListCmd)
	usersCmd.AddCommand(usersListCmd)
}

// userStatusFlag: the search/users enum is bare (ENABLED, not
// STATUS_ENABLED like app_users), so there is no prefix to pass a newer value
// through on.
var userStatusFlag = enumFlag{flag: "status", unknown: enumPassAny, values: []enumValue{
	{name: "enabled", api: "ENABLED"},
	{name: "disabled", api: "DISABLED"},
	{name: "deleted", api: "DELETED"},
}}

// userOriginFlag lists origins in the order "all" expands to.
var userOriginFlag = enumFlag{flag: "origin", prefix: "USER_ORIGIN_", unknown: enumPassPrefixed, keywords: []string{"all"}, values: []enumValue{
	{name: "directory", api: "USER_ORIGIN_DIRECTORY"},
	{name: "local", api: "USER_ORIGIN_LOCAL"},
	{name: "system", api: "USER_ORIGIN_SYSTEM"},
}}

// userOrigins reads --origin into the API enum, deduped and in a stable
// order; nil when unset, so the server's default applies. A USER_ORIGIN_*
// name passes through, so a row's origin value round-trips.
func userOrigins(cmd *cobra.Command) ([]string, error) {
	values, err := repeatableStringFlag(cmd, "origin")
	if err != nil || len(values) == 0 {
		return nil, err
	}
	want := map[string]bool{}
	var extra []string
	for _, raw := range values {
		api, err := userOriginFlag.parse(raw)
		if err != nil {
			return nil, err
		}
		if api == "all" {
			for _, o := range userOriginFlag.values {
				want[o.api] = true
			}
			continue
		}
		if !want[api] {
			want[api] = true
			extra = append(extra, api)
		}
	}
	var out []string
	for _, o := range userOriginFlag.values {
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
