package cmd

import (
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
		"id":           nilIfEmpty(u.ID),
		"display_name": nilIfEmpty(u.DisplayName),
		"email":        nilIfEmpty(u.Email),
		"username":     nilIfEmpty(u.Username),
		"status":       nilIfEmpty(u.Status),
		"job_title":    nilIfEmpty(u.JobTitle),
		"deleted_at":   nilIfEmpty(u.DeletedAt),
	}
}

var appsOwnersCmd = &cobra.Command{
	Use:   "owners <app-id>",
	Short: "List an app's owners (NDJSON output)",
	Long: `List the owners of an app via GET .../owners -- the read that reflects what
"apps set-owners"/"apps add-owner"/"apps remove-owner" write. Use this to
verify a write.
` + ownerAsyncNote + `
An owner added moments ago may not appear yet.

Expect at least one owner on most apps even before you ever run "add-owner"
or "set-owners": "apps create" auto-assigns its caller as an owner.

Auto-paginates to completion like every other list command.

A well-formed app id that does not exist returns zero rows and exit 0, not a
404 -- the endpoint answers 200 with an empty list. An empty result here is
either "no owners" or "wrong id"; "apps add-owner"/"apps remove-owner" on the
same id exit 4 and can tell you which.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return listOwners(cmd, client.Path("/api/v1/apps/%s/owners", args[0]))
	},
}

// listOwners streams GET path (an app's or an entitlement's .../owners) as
// NDJSON owner rows, auto-paginating with the stuck-cursor guard.
func listOwners(cmd *cobra.Command, path string) error {
	c, err := listClientFor(cmd)
	if err != nil {
		return err
	}
	return listPages(cmd, c, pageSpec[appOwner]{
		Route: "GET " + path,
		Fetch: getPage(cmd.Context(), path, nil),
		Parse: parseList[appOwner],
		ID:    func(o appOwner) string { return o.ID },
		Rows:  rowFn(appOwnerRow),
	})
}

func init() {
	addPaginationFlags(appsOwnersCmd)
	appsCmd.AddCommand(appsOwnersCmd)
}
