package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

// managedStateBinding is the managed-state binding of one discovered app.
type managedStateBinding struct {
	ResourceID  string `json:"resourceId"`
	DisplayName string `json:"displayName"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
	State       *struct {
		Managed *struct {
			AppID string `json:"appId"`
		} `json:"managed"`
		Unmanaged *struct{} `json:"unmanaged"`
	} `json:"state"`
}

// managedStateListItem's binding is a pointer because the API may send it null.
type managedStateListItem struct {
	Binding *managedStateBinding `json:"appManagementStateBinding"`
}

// managedStateRow flattens a binding into the NDJSON output row. state is
// "managed", "unmanaged", or nil if the API sent neither; managed_app_id is
// nil unless managed.
func managedStateRow(b managedStateBinding) map[string]any {
	state, managedAppID := "", ""
	if s := b.State; s != nil {
		switch {
		case s.Managed != nil:
			state, managedAppID = "managed", s.Managed.AppID
		case s.Unmanaged != nil:
			state = "unmanaged"
		}
	}
	return map[string]any{
		"resource_id":    nilIfEmpty(b.ResourceID),
		"display_name":   nilIfEmpty(b.DisplayName),
		"state":          nilIfEmpty(state),
		"managed_app_id": nilIfEmpty(managedAppID),
		"created_at":     nilIfEmpty(b.CreatedAt),
		"updated_at":     nilIfEmpty(b.UpdatedAt),
	}
}

var appsManagedStateListCmd = &cobra.Command{
	Use:   "list",
	Short: "List a connector's discovered apps and their managed state (NDJSON output)",
	Long: `List the apps a connector discovered and whether each is managed (NDJSON output).
Rows carry resource_id (pass it to "managed-state get" and "managed-state
promote"), display_name, state ("unmanaged" or "managed"), and managed_app_id
(the managed app's id; null while unmanaged).

A well-formed but unknown app or resource type returns zero rows and exit 0: the
endpoint answers 200 with an empty list. "managed-state get" on the same ids
exits 4.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newListClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		path := managedStateBindingsPath(cmd)
		requestedPageSize := pageSizeFlag(cmd)
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		enc := newEmitter(cmd)
		guard := newPaginationCursorGuard().withCompositeIDs() // the wrapper may be null
		route := "GET " + path
		for !limitReached(enc.Written(), limit) {
			if guard.tokenAlreadyUsed(pageToken) {
				return guard.warnStuck(cmd, route)
			}
			pageSize := requestedPageSize
			if !enc.Filtered() {
				pageSize = effectivePageSize(requestedPageSize, limit, enc.Written())
			}
			params := map[string]string{"page_size": strconv.Itoa(pageSize)}
			if pageToken != "" {
				params["page_token"] = pageToken
			}

			data, err := c.Get(cmd.Context(), path, params)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List          []managedStateListItem `json:"list"`
				NextPageToken string                 `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			ids := make([]string, len(resp.List))
			for i, it := range resp.List {
				if it.Binding != nil {
					ids[i] = it.Binding.ResourceID
				}
			}
			keep, err := guard.checkPage(cmd, route, ids)
			if err != nil {
				return err
			}
			for _, item := range keepRows(resp.List, keep) {
				// A missing wrapper is skipped rather than emitted as an all-null row.
				if item.Binding == nil {
					continue
				}
				if err := enc.Encode(managedStateRow(*item.Binding)); err != nil {
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
	addManagedStateScopeFlags(appsManagedStateListCmd)
	addPaginationFlags(appsManagedStateListCmd)
	appsManagedStateCmd.AddCommand(appsManagedStateListCmd)
}
