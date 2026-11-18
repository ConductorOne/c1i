package cmd

import (
	"github.com/spf13/cobra"
)

const appResourcesSearchPath = "/api/v1/search/app_resources"

var appsResourcesSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search resources across apps (NDJSON output)",
	Long: `Search resources across the tenant, or across the apps named by --app-id
(NDJSON output). --query is a fuzzy match on the name. The id flags keep or
drop resources by resource type, resource, or resource type trait (the
trait_ids of an "apps resource-types" row). --owner-user-id keeps resources
owned by any of the given users; --unowned-only keeps those with no owner.
Repeatable flags may be given more than once; several values of one flag
match any of them.

Rows have the same shape as "apps resources list"; a deleted resource is not
returned. Search can trail a write by a few seconds: a resource updated
moments earlier came back with its old values.
One resource id can appear on several apps (a row per app), so key rows on
app_id, resource_type_id and id.

An id that matches nothing returns zero rows and exit 0; an id that is not 27
letters and digits is rejected by the server (exit 2).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		body := map[string]any{}
		addStringsToBody(cmd, body, [][2]string{{"query", "query"}})
		if err := addRepeatableToBody(cmd, body, [][2]string{
			{"app-id", "appIds"},
			{"resource-type-id", "resourceTypeIds"},
			{"resource-id", "resourceIds"},
			{"exclude-resource-id", "excludeResourceIds"},
			{"trait-id", "resourceTypeTraitIds"},
			{"exclude-trait-id", "excludeResourceTypeTraitIds"},
			{"owner-user-id", "ownerUserIds"},
		}); err != nil {
			return err
		}
		if v, _ := cmd.Flags().GetBool("unowned-only"); v {
			body["unownedOnly"] = true
		}
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		return listPages(cmd, c, pageSpec[resourceListItem]{
			Route: "POST " + appResourcesSearchPath,
			Fetch: postPage(cmd.Context(), appResourcesSearchPath, body),
			Parse: parseList[resourceListItem],
			ID: func(it resourceListItem) string {
				r := it.AppResource
				return scopedRowID(r.AppID, r.ResourceTypeID, r.ID)
			},
			Rows:      rowFn(resourceRow),
			Composite: true,
		})
	},
}

func init() {
	c := appsResourcesSearchCmd
	c.Flags().String("query", "", "Fuzzy search on the display name")
	addRepeatableStringFlag(c, "app-id", "Only resources of this app (repeatable)")
	addRepeatableStringFlag(c, "resource-type-id", "Only resources of this resource type (repeatable)")
	addRepeatableStringFlag(c, "resource-id", "Only this resource id (repeatable)")
	addRepeatableStringFlag(c, "exclude-resource-id", "Drop this resource id (repeatable)")
	addRepeatableStringFlag(c, "trait-id", "Only resources whose type has this trait id (repeatable)")
	addRepeatableStringFlag(c, "exclude-trait-id", "Drop resources whose type has this trait id (repeatable)")
	addRepeatableStringFlag(c, "owner-user-id", "Only resources owned by this C1 user (repeatable)")
	c.Flags().Bool("unowned-only", false, "Only resources with no owner")
	addPaginationFlags(c)
	appsResourcesCmd.AddCommand(c)
}
