package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var xaaAccessProfiles = xaaKind{
	noun:     "access-profile",
	label:    "access profile",
	segment:  "access_profiles",
	envelope: "accessProfile",
	listKey:  "accessProfiles",
	row:      xaaAccessProfileRow,
	required: []string{"displayName", "xaaResourceServerId"},
}

var xaaAccessProfilesCmd = &cobra.Command{
	Use:   "access-profiles",
	Short: "Manage XAA access profiles",
	Long: `Manage an app's cross-app access profiles: requestable bundles of one
resource server's scopes. Each profile has a backing entitlement
(appEntitlementId) that users request. Unrelated to the request-catalog
"c1i access-profiles".`,
}

// xaaAccessProfileRow is the NDJSON row of "xaa access-profiles list".
func xaaAccessProfileRow(item map[string]any) map[string]any {
	row := xaaRowBase(item)
	row["resource_server_id"] = optString(item, "xaaResourceServerId")
	row["app_entitlement_id"] = optString(item, "appEntitlementId")
	row["scope_count"] = item["scopeCount"]
	return row
}

var xaaAccessProfilesListCmd = xaaAccessProfiles.listCmd("List an app's XAA access profiles (NDJSON output)",
	`List the cross-app access profiles of an app (NDJSON output), one row per
profile: id, app_id, display_name, description, resource_server_id,
app_entitlement_id, scope_count, created_at, updated_at. A deleted profile is
not listed.

A well-formed but unknown app returns zero rows and exit 0.`)

var xaaAccessProfilesGetCmd = xaaAccessProfiles.getCmd("Show one XAA access profile (pretty JSON)",
	`Show one cross-app access profile, unwrapped. A deleted profile is still
returned, with deletedAt set. An unknown id exits 4.`)

var xaaAccessProfilesForEntitlementCmd = &cobra.Command{
	Use:   "for-entitlement <entitlement-id>",
	Short: "Show the XAA access profile behind an entitlement (pretty JSON)",
	Long: `Show the cross-app access profile whose backing entitlement is
<entitlement-id>, unwrapped like "xaa access-profiles get". An entitlement
with no profile exits 4; so did a profile created seconds earlier, in the run
checked.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return getXAAObject(cmd, client.Path("/api/v1/apps/%s/xaa/access_profiles/by_app_entitlement_id/%s", xaaAppID(cmd), args[0]), "accessProfile")
	},
}

var xaaAccessProfilesCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an XAA access profile (pretty JSON)",
	Long: `Create a cross-app access profile under a resource server. The server
provisions a backing entitlement (appEntitlementId). Display names need not be
unique.

The resource server is looked up first under --app-id: if it does not exist
the command exits 4, and if it is deleted, 2 (the server would accept both).
--dry-run does the lookup too.

Prints the created profile, unwrapped like "xaa access-profiles get": the id
is .id. Honors --dry-run.

Example:
  c1i xaa access-profiles create --app-id <app-id> \
    --resource-server-id <resource-server-id> --display-name "Orders read-only"`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		appID := xaaAppID(cmd)
		rsID := mustGetString(cmd, "resource-server-id")
		body := map[string]any{
			"xaaResourceServerId": rsID,
			"displayName":         mustGetString(cmd, "display-name"),
		}
		if _, err := xaaAccessProfileUpdateSpec.apply(cmd, body); err != nil {
			return err
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: xaaAccessProfiles.collectionPath(appID), Body: body, Unwrap: named("accessProfile")},
			withPreRead(xaaResourceServerCheck(appID, rsID)))
	},
}

var xaaAccessProfileUpdateSpec = &updateSpec{
	fields: []updateField{
		strUpdate("display-name", "displayName"),
		strUpdate("description", "description"),
	},
	scope: []string{"app-id"},
}

var xaaAccessProfilesUpdateCmd = &cobra.Command{
	Use:   "update <access-profile-id>",
	Short: "Update an XAA access profile (pretty JSON)",
	Long: `Update a cross-app access profile's display name or description. Only the
flags you pass change.

The server validates displayName and xaaResourceServerId on every update,
whatever the mask names, so c1i reads the profile first and fills in the ones
the request leaves unset; the mask still decides what changes. That read makes
an unknown id exit 4 before the update is sent, --dry-run included.

Prints the updated profile, unwrapped like "xaa access-profiles get".`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appID := xaaAppID(cmd)
		body, obj, err := buildXAAUpdate(cmd, xaaAccessProfiles, xaaAccessProfileUpdateSpec)
		if err != nil {
			return err
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: xaaAccessProfiles.itemPath(appID, args[0]), Body: body, Unwrap: named("accessProfile")},
			withPreRead(xaaAccessProfiles.xaaRequiredFill(appID, args[0], obj)))
	},
}

var xaaAccessProfilesDeleteCmd = xaaAccessProfiles.deleteCmd("Delete an XAA access profile",
	`Delete a cross-app access profile. Honors --dry-run.

The delete is soft: "xaa access-profiles get" still returns the profile with
deletedAt set, and "list" stops listing it. Repeating the delete exits 0; an
unknown id exits 4. Its backing entitlement was deleted within seconds in the
run checked; the API spec says outstanding grants end with it.

Prints the API's response, which is {}; exit 0 is the success signal.`)

var xaaAccessProfilesHistoryCmd = xaaAccessProfiles.historyCmd("List an XAA access profile's change history (NDJSON output)",
	xaaHistoryLong("access profile", "xaa access-profiles get"))

func init() {
	fe := xaaAccessProfilesForEntitlementCmd
	addXAAAppFlag(fe)

	cr := xaaAccessProfilesCreateCmd
	addXAAAppFlag(cr)
	cr.Flags().String("resource-server-id", "", "ID of the resource server whose scopes the profile bundles")
	cr.Flags().String("display-name", "", "Display name")
	cr.Flags().String("description", "", "Description")
	markRequired(cr, "resource-server-id", "display-name")
	markMutating(cr)

	up := xaaAccessProfilesUpdateCmd
	addXAAAppFlag(up)
	up.Flags().String("display-name", "", "New display name")
	up.Flags().String("description", "", xaaDescriptionUpdateUsage)
	registerUpdateSpec(up, xaaAccessProfileUpdateSpec)
	allowEmpty(up, emptyIsSent, "description")
	markMutating(up)

	xaaAccessProfilesCmd.AddCommand(xaaAccessProfilesListCmd, xaaAccessProfilesGetCmd, fe, cr, up, xaaAccessProfilesDeleteCmd, xaaAccessProfilesHistoryCmd)
	xaaCmd.AddCommand(xaaAccessProfilesCmd)
}
