package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// appOwner is the subset of the User message surfaced by GET .../owners.
// The endpoint returns full User objects (also emails[], createdAt,
// updatedAt, managerIds[]); this struct only decodes what appOwnerRow uses.
type appOwner struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	Username    string `json:"username"`
	Status      string `json:"status"`
	JobTitle    string `json:"jobTitle"`
	DeletedAt   string `json:"deletedAt"`
}

// appOwnerRow flattens an appOwner into the NDJSON output row. Mirrors the
// person projection in users_list.go, with two changes. username replaces
// department: the endpoint returns both, but department was empty on every
// owner observed. deleted_at is added because an app's owner can itself be a
// deleted C1 user, which "who owns this app" should flag; it is nil, not "",
// on a live user -- see CLAUDE.md's row-fidelity convention.
func appOwnerRow(u appOwner) map[string]any {
	return map[string]any{
		"id":           u.ID,
		"display_name": u.DisplayName,
		"email":        u.Email,
		"username":     u.Username,
		"status":       u.Status,
		"job_title":    u.JobTitle,
		"deleted_at":   nilIfEmpty(u.DeletedAt),
	}
}

var appsOwnersCmd = &cobra.Command{
	Use:   "owners <app-id>",
	Short: "List an app's owners (NDJSON output)",
	Long: `List the owners of an app via GET .../owners -- the v1 owner store that
"apps set-owners"/"apps add-owner"/"apps remove-owner" write to. Use this to
verify a write: it lags the write it's confirming by roughly 45-150s
(owner provisioning is asynchronous), so an owner added moments ago may not
appear yet.

Expect at least one owner on most apps even before you ever run "add-owner"
or "set-owners": "apps create" auto-assigns its caller as an owner.

Auto-paginates to completion like every other list command.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appID := args[0]
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newListClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

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
			params := map[string]string{
				"page_size": strconv.Itoa(pageSize),
			}
			if pageToken != "" {
				params["page_token"] = pageToken
			}

			data, err := c.Get(cmd.Context(), client.Path("/api/v1/apps/%s/owners", appID), params)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List          []appOwner `json:"list"`
				NextPageToken string     `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, o := range resp.List {
				_ = enc.Encode(appOwnerRow(o))
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
	appsOwnersCmd.Flags().Int("page-size", 50, "Results per page (max 100)")
	appsOwnersCmd.Flags().String("page-token", "", "Pagination cursor")
	addLimitFlag(appsOwnersCmd)
	appsCmd.AddCommand(appsOwnersCmd)
}
