package cmd

import (
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
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		path := managedStateBindingsPath(cmd)
		return listPages(cmd, c, pageSpec[managedStateListItem]{
			Route:     "GET " + path,
			Fetch:     getPage(cmd.Context(), path, nil),
			Parse:     parseList[managedStateListItem],
			Composite: true, // the wrapper may be null
			ID: func(it managedStateListItem) string {
				if it.Binding == nil {
					return ""
				}
				return it.Binding.ResourceID
			},
			// A missing wrapper is skipped rather than emitted as an all-null row.
			Rows: func(it managedStateListItem) []any {
				if it.Binding == nil {
					return nil
				}
				return []any{managedStateRow(*it.Binding)}
			},
		})
	},
}

func init() {
	addManagedStateScopeFlags(appsManagedStateListCmd)
	addPaginationFlags(appsManagedStateListCmd)
	appsManagedStateCmd.AddCommand(appsManagedStateListCmd)
}
