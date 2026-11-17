package cmd

import (
	"github.com/spf13/cobra"
)

// appListItem is the subset of the App message surfaced in `apps list` rows.
type appListItem struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"displayName"`
	Description string    `json:"description"`
	UserCount   flexInt64 `json:"userCount"`
	IconURL     string    `json:"iconUrl"`
}

// appRow flattens an appListItem into the NDJSON output row. user_count keeps
// its real numeric type (the API sends it as a JSON string); there is no
// deleted_at, because the endpoint omits deleted apps.
func appRow(a appListItem) map[string]any {
	return map[string]any{
		"id":           nilIfEmpty(a.ID),
		"display_name": nilIfEmpty(a.DisplayName),
		"description":  nilIfEmpty(a.Description),
		"user_count":   int64(a.UserCount),
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
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		return listPages(cmd, c, pageSpec[appListItem]{
			Route: "GET /api/v1/apps",
			Fetch: getPage(cmd.Context(), "/api/v1/apps", nil),
			Parse: parseList[appListItem],
			ID:    func(a appListItem) string { return a.ID },
			Rows:  rowFn(appRow),
		})
	},
}

func init() {
	addPaginationFlags(appsListCmd)
	appsCmd.AddCommand(appsListCmd)
}
