package cmd

import (
	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var appsResourceTypesCmd = &cobra.Command{
	Use:   "resource-types",
	Short: "Manage an app's resource types",
	Long: `Manage an app's resource types: list, search, get, and create, rename or
delete the manually managed ones. A resource type groups an app's resources
("apps resources"); each type belongs to the app given by --app-id.`,
}

// appResourceType is the AppResourceType message, as far as a row needs it.
type appResourceType struct {
	ID          string   `json:"id"`
	AppID       string   `json:"appId"`
	DisplayName string   `json:"displayName"`
	TraitIDs    []string `json:"traitIds"`
}

// resourceTypeListItem is one row of GET /api/v1/apps/{app-id}/resource_types.
type resourceTypeListItem struct {
	AppResourceType appResourceType `json:"appResourceType"`
}

// resourceTypeRow is the NDJSON row of "resource-types list" and "search".
// trait_ids is nil, not [], on a type with no traits.
func resourceTypeRow(t appResourceType) map[string]any {
	var traits any
	if len(t.TraitIDs) > 0 {
		traits = t.TraitIDs
	}
	return map[string]any{
		"id":           nilIfEmpty(t.ID),
		"app_id":       nilIfEmpty(t.AppID),
		"display_name": nilIfEmpty(t.DisplayName),
		"trait_ids":    traits,
	}
}

// resourceTypesPath is the collection of one app's resource types.
func resourceTypesPath(appID string) string {
	return client.Path("/api/v1/apps/%s/resource_types", appID)
}

// resourceTypePath addresses one resource type of an app.
func resourceTypePath(appID, id string) string {
	return client.Path("/api/v1/apps/%s/resource_types/%s", appID, id)
}

// addResourceTypeAppFlag registers the parent --app-id every per-app
// resource-types subcommand takes.
func addResourceTypeAppFlag(cmd *cobra.Command) {
	cmd.Flags().String("app-id", "", "ID of the app the resource type belongs to")
	markRequired(cmd, "app-id")
}

var appsResourceTypesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List an app's resource types (NDJSON output)",
	Long: `List the resource types of an app (NDJSON output): the id to pass as
--resource-type-id to "apps resources" and "apps managed-state". A connector
app's discovered apps are in its resource type named "app". Rows have the same
shape as "apps resource-types search". A deleted type is not listed.

A well-formed but unknown app returns zero rows and exit 0.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		appID, _ := cmd.Flags().GetString("app-id")
		path := resourceTypesPath(appID)
		return listPages(cmd, c, pageSpec[resourceTypeListItem]{
			Route: "GET " + path,
			Fetch: getPage(cmd.Context(), path, nil),
			Parse: parseList[resourceTypeListItem],
			ID:    func(it resourceTypeListItem) string { return it.AppResourceType.ID },
			Rows:  rowFn(func(it resourceTypeListItem) map[string]any { return resourceTypeRow(it.AppResourceType) }),
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
