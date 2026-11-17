package cmd

import (
	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var appsResourceTypesCmd = &cobra.Command{
	Use:   "resource-types",
	Short: "Inspect an app's resource types",
}

// resourceTypeListItem is one row of GET /api/v1/apps/{app-id}/resource_types.
type resourceTypeListItem struct {
	AppResourceType struct {
		ID          string   `json:"id"`
		DisplayName string   `json:"displayName"`
		TraitIDs    []string `json:"traitIds"`
	} `json:"appResourceType"`
}

// resourceTypeRow flattens a resourceTypeListItem into the NDJSON output row.
// trait_ids is nil, not [], on a type with no traits.
func resourceTypeRow(item resourceTypeListItem) map[string]any {
	t := item.AppResourceType
	var traits any
	if len(t.TraitIDs) > 0 {
		traits = t.TraitIDs
	}
	return map[string]any{
		"id":           nilIfEmpty(t.ID),
		"display_name": nilIfEmpty(t.DisplayName),
		"trait_ids":    traits,
	}
}

var appsResourceTypesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List an app's resource types (NDJSON output)",
	Long: `List the resource types of an app (NDJSON output): the id to pass as
--resource-type-id to "apps managed-state". A connector app's discovered apps
are in its resource type named "app".

A well-formed but unknown app returns zero rows and exit 0.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		appID, _ := cmd.Flags().GetString("app-id")
		path := client.Path("/api/v1/apps/%s/resource_types", appID)
		return listPages(cmd, c, pageSpec[resourceTypeListItem]{
			Route: "GET " + path,
			Fetch: getPage(cmd.Context(), path, nil),
			Parse: parseList[resourceTypeListItem],
			ID:    func(it resourceTypeListItem) string { return it.AppResourceType.ID },
			Rows:  rowFn(resourceTypeRow),
		})
	},
}

func init() {
	appsResourceTypesListCmd.Flags().String("app-id", "", "Application ID")
	addPaginationFlags(appsResourceTypesListCmd)
	markRequired(appsResourceTypesListCmd, "app-id")
	appsResourceTypesCmd.AddCommand(appsResourceTypesListCmd)
	appsCmd.AddCommand(appsResourceTypesCmd)
}
