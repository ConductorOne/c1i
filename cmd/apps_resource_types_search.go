package cmd

import (
	"github.com/spf13/cobra"
)

const appResourceTypesSearchPath = "/api/v1/search/app_resource_types"

var appsResourceTypesSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search resource types across apps (NDJSON output)",
	Long: `Search resource types across the tenant, or across the apps named by --app-id
(NDJSON output). --query is a fuzzy match, --display-name a case-insensitive
display-name match. The id flags keep or drop resource types by id or by
trait id (the trait_ids of a row). Repeatable flags may be given more than
once; several values of one flag match any of them.

Rows have the same shape as "apps resource-types list"; a deleted type is not
returned, though search trails a delete by a few seconds. One resource type id can appear on several apps (a row per app), so
key rows on app_id and id.

An id that matches nothing returns zero rows and exit 0; an id that is not 27
letters and digits is rejected by the server (exit 2).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		body := map[string]any{}
		addStringsToBody(cmd, body, [][2]string{{"query", "query"}, {"display-name", "displayName"}})
		if err := addRepeatableToBody(cmd, body, [][2]string{
			{"app-id", "appIds"},
			{"resource-type-id", "resourceTypeIds"},
			{"exclude-resource-type-id", "excludeResourceTypeIds"},
			{"trait-id", "resourceTypeTraitIds"},
			{"exclude-trait-id", "excludeResourceTypeTraitIds"},
		}); err != nil {
			return err
		}
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		return listPages(cmd, c, pageSpec[appResourceType]{
			Route:     "POST " + appResourceTypesSearchPath,
			Fetch:     postPage(cmd.Context(), appResourceTypesSearchPath, body),
			Parse:     parseList[appResourceType],
			ID:        func(t appResourceType) string { return scopedRowID(t.AppID, t.ID) },
			Rows:      rowFn(resourceTypeRow),
			Composite: true,
		})
	},
}

// addStringsToBody sets body[key] to each {flag, key} string flag that is
// non-empty.
func addStringsToBody(cmd *cobra.Command, body map[string]any, pairs [][2]string) {
	for _, p := range pairs {
		if v, _ := cmd.Flags().GetString(p[0]); v != "" {
			body[p[1]] = v
		}
	}
}

// addRepeatableToBody sets body[key] to each {flag, key} repeatable flag's
// values, in order, so the first bad flag is the one reported.
func addRepeatableToBody(cmd *cobra.Command, body map[string]any, pairs [][2]string) error {
	for _, p := range pairs {
		flag, key := p[0], p[1]
		vals, err := repeatableStringFlag(cmd, flag)
		if err != nil {
			return err
		}
		if len(vals) > 0 {
			body[key] = vals
		}
	}
	return nil
}

func init() {
	c := appsResourceTypesSearchCmd
	c.Flags().String("query", "", "Fuzzy search on the display name")
	c.Flags().String("display-name", "", "Case-insensitive display name match")
	addRepeatableStringFlag(c, "app-id", "Only resource types of this app (repeatable)")
	addRepeatableStringFlag(c, "resource-type-id", "Only this resource type id (repeatable)")
	addRepeatableStringFlag(c, "exclude-resource-type-id", "Drop this resource type id (repeatable)")
	addRepeatableStringFlag(c, "trait-id", "Only resource types with this trait id (repeatable)")
	addRepeatableStringFlag(c, "exclude-trait-id", "Drop resource types with this trait id (repeatable)")
	addPaginationFlags(c)
	appsResourceTypesCmd.AddCommand(c)
}
