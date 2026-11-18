package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// appRefShape names an API's {appId, <id>} pair: an id scoped to its app, so
// neither field can be omitted. APIs key the second field differently
// (AppEntitlementRef "id", access-review setup inputs "appEntitlementId").
type appRefShape struct{ name, idKey string }

var appEntitlementRefShape = appRefShape{name: "AppEntitlementRef", idKey: "id"}

// appRef marshals as {"appId":…,<shape.idKey>:…}, appId first.
type appRef struct {
	AppID, ID string
	shape     appRefShape
}

func (r appRef) MarshalJSON() ([]byte, error) {
	appID, _ := json.Marshal(r.AppID) // a string always marshals
	key, _ := json.Marshal(r.shape.idKey)
	id, _ := json.Marshal(r.ID)
	return []byte(`{"appId":` + string(appID) + `,` + string(key) + `:` + string(id) + `}`), nil
}

var accessProfilesRequestableEntitlementsCmd = &cobra.Command{
	Use:   "requestable-entitlements",
	Short: "Manage entitlements users can request from an access profile",
}

var accessProfilesVisibilityEntitlementsCmd = &cobra.Command{
	Use:   "visibility-entitlements",
	Short: "Manage entitlements required to see an access profile",
}

var accessProfilesRequestableEntitlementsListCmd = newAccessProfileEntitlementListCmd(
	"List entitlements users can request from an access profile (NDJSON output)",
	"requestable_entitlements",
)

var accessProfilesVisibilityEntitlementsListCmd = newAccessProfileEntitlementListCmd(
	"List entitlements required to see an access profile (NDJSON output)",
	"visibility_entitlements",
)

var accessProfilesRequestableEntitlementIDsListCmd = &cobra.Command{
	Use:   "list-ids <access-profile-id>",
	Short: "List requestable entitlement references for an access profile (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		path := client.Path("/api/v1/catalogs/%s/requestable_entitlementIDs", args[0])
		data, err := c.Get(cmd.Context(), path, nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeObject(cmd, data)
	},
}

var accessProfilesRequestableEntitlementsAddCmd = &cobra.Command{
	Use:   "add <access-profile-id>",
	Short: "Add requestable entitlements to an access profile (prints {})",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		refs, err := catalogEntitlementReferences(cmd, true)
		if err != nil {
			return err
		}
		body := buildRequestableEntitlementsBody(cmd, refs)
		path := client.Path("/api/v1/catalogs/%s/requestable_entries", args[0])
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: body})
	},
}

var accessProfilesRequestableEntitlementsRemoveCmd = &cobra.Command{
	Use:   "remove <access-profile-id>",
	Short: "Remove requestable entitlements from an access profile (prints {})",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		refs, err := catalogEntitlementReferences(cmd, true)
		if err != nil {
			return err
		}
		path := client.Path("/api/v1/catalogs/%s/requestable_entries", args[0])
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: path, Body: buildRequestableEntitlementsBody(cmd, refs)})
	},
}

var accessProfilesRequestableEntitlementsSetCmd = &cobra.Command{
	Use:   "set <access-profile-id>",
	Short: "Replace all requestable entitlements (prints {})",
	Long: `Replace the full requestable-entitlement set in an access profile.

Pass --refs-file with a JSON array of {"appId":"…","id":"…"} objects. Every
existing requestable entitlement not in that array is removed; pass [] to
clear the entire set.

Prints the API's response, which is {}; exit 0 is the success signal.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		refs, err := catalogEntitlementReferencesFromFile(cmd)
		if err != nil {
			return err
		}
		body := buildRequestableEntitlementsBody(cmd, refs)
		path := client.Path("/api/v1/catalogs/%s/requestable_entitlements/update", args[0])
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: body})
	},
}

var accessProfilesVisibilityEntitlementsAddCmd = &cobra.Command{
	Use:   "add <access-profile-id>",
	Short: "Add visibility entitlements to an access profile (prints {})",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		refs, err := catalogEntitlementReferences(cmd, true)
		if err != nil {
			return err
		}
		path := client.Path("/api/v1/catalogs/%s/visibility_bindings", args[0])
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: buildVisibilityEntitlementsBody(refs)})
	},
}

var accessProfilesVisibilityEntitlementsRemoveCmd = &cobra.Command{
	Use:   "remove <access-profile-id>",
	Short: "Remove visibility entitlements from an access profile (prints {})",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		refs, err := catalogEntitlementReferences(cmd, true)
		if err != nil {
			return err
		}
		path := client.Path("/api/v1/catalogs/%s/visibility_bindings", args[0])
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: path, Body: buildVisibilityEntitlementsBody(refs)})
	},
}

// newAccessProfileEntitlementListCmd creates the two identical paginated views
// of AppEntitlementView objects: one for requestability and one for visibility.
func newAccessProfileEntitlementListCmd(short, endpoint string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list <access-profile-id>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := listClientFor(cmd)
			if err != nil {
				return err
			}
			path := client.Path("/api/v1/catalogs/%s/"+endpoint, args[0])
			return listPages(cmd, c, pageSpec[entitlementListItem]{
				Route:     "GET " + path,
				Fetch:     getPage(cmd.Context(), path, nil),
				Parse:     parseList[entitlementListItem],
				ID:        entitlementGuardID,
				Rows:      rowFn(entitlementRow),
				Composite: true,
			})
		},
	}
	addPaginationFlags(cmd)
	return cmd
}

func catalogEntitlementReferences(cmd *cobra.Command, required bool) ([]appRef, error) {
	return appRefsFromFlags(cmd, required, appEntitlementRefShape)
}

// appRefsFromFlags pairs --app-id with each repeated --entitlement-id.
func appRefsFromFlags(cmd *cobra.Command, required bool, shape appRefShape) ([]appRef, error) {
	ids, err := repeatableStringFlag(cmd, "entitlement-id")
	if err != nil {
		return nil, err
	}
	if required && len(ids) == 0 {
		return nil, &usageError{fmt.Errorf("flag --entitlement-id requires at least one value")}
	}
	appID, _ := cmd.Flags().GetString("app-id")
	refs := make([]appRef, len(ids))
	for i, id := range ids {
		refs[i] = appRef{AppID: appID, ID: id, shape: shape}
	}
	return refs, nil
}

func catalogEntitlementReferencesFromFile(cmd *cobra.Command) ([]appRef, error) {
	refsFile, _ := cmd.Flags().GetString("refs-file")
	if refsFile == "" {
		return nil, &usageError{fmt.Errorf("--refs-file is required")}
	}
	refs, err := readJSONFile[[]any](cmd, "refs-file", refsFile)
	if err != nil {
		return nil, err
	}
	return parseAppRefs(refs, "refs", appEntitlementRefShape)
}

func validateAppEntitlementRefs(raw []any, field string) error {
	_, err := parseAppRefs(raw, field, appEntitlementRefShape)
	return err
}

// parseAppRefs requires each element to hold exactly appId and shape.idKey,
// both non-empty strings.
func parseAppRefs(raw []any, field string, shape appRefShape) ([]appRef, error) {
	elems, err := objectElements(field, raw)
	if err != nil {
		return nil, err
	}
	refs := make([]appRef, len(elems))
	for i, ref := range elems {
		if len(ref) != 2 {
			return nil, &usageError{fmt.Errorf("%s[%d] must be an %s with exactly appId and %s", field, i, shape.name, shape.idKey)}
		}
		appID, appOK := ref["appId"].(string)
		id, idOK := ref[shape.idKey].(string)
		if !appOK || !idOK || appID == "" || id == "" {
			return nil, &usageError{fmt.Errorf("%s[%d] must be an %s with non-empty appId and %s", field, i, shape.name, shape.idKey)}
		}
		refs[i] = appRef{AppID: appID, ID: id, shape: shape}
	}
	return refs, nil
}

func buildRequestableEntitlementsBody(cmd *cobra.Command, refs []appRef) map[string]any {
	body := map[string]any{"appEntitlements": refs}
	if flag := cmd.Flags().Lookup("create-requests"); flag != nil && flag.Changed {
		createRequests, _ := cmd.Flags().GetBool("create-requests")
		body["createRequests"] = createRequests
	}
	return body
}

func buildVisibilityEntitlementsBody(refs []appRef) map[string]any {
	return map[string]any{"accessEntitlements": refs}
}

func init() {
	for _, cmd := range []*cobra.Command{
		accessProfilesRequestableEntitlementsAddCmd,
		accessProfilesRequestableEntitlementsRemoveCmd,
		accessProfilesVisibilityEntitlementsAddCmd,
		accessProfilesVisibilityEntitlementsRemoveCmd,
	} {
		cmd.Flags().String("app-id", "", "Application ID")
		addRepeatableStringFlag(cmd, "entitlement-id", "Application entitlement ID (repeatable)")
		markRequired(cmd, "app-id")
		annotateRequired(cmd, "entitlement-id")
	}
	accessProfilesRequestableEntitlementsSetCmd.Flags().String("refs-file", "", "JSON array of AppEntitlementRef objects (file, or \"-\" for stdin)")
	annotateRequired(accessProfilesRequestableEntitlementsSetCmd, "refs-file")
	accessProfilesRequestableEntitlementsAddCmd.Flags().Bool("create-requests", false, "Create requests for users in the access profile when adding these entitlements")

	for _, c := range []*cobra.Command{
		accessProfilesRequestableEntitlementsAddCmd,
		accessProfilesRequestableEntitlementsRemoveCmd,
		accessProfilesRequestableEntitlementsSetCmd,
		accessProfilesVisibilityEntitlementsAddCmd,
		accessProfilesVisibilityEntitlementsRemoveCmd,
	} {
		markMutating(c)
	}
	accessProfilesRequestableEntitlementsCmd.AddCommand(
		accessProfilesRequestableEntitlementsListCmd,
		accessProfilesRequestableEntitlementIDsListCmd,
		accessProfilesRequestableEntitlementsAddCmd,
		accessProfilesRequestableEntitlementsRemoveCmd,
		accessProfilesRequestableEntitlementsSetCmd,
	)
	accessProfilesVisibilityEntitlementsCmd.AddCommand(
		accessProfilesVisibilityEntitlementsListCmd,
		accessProfilesVisibilityEntitlementsAddCmd,
		accessProfilesVisibilityEntitlementsRemoveCmd,
	)
	accessProfilesCmd.AddCommand(accessProfilesRequestableEntitlementsCmd, accessProfilesVisibilityEntitlementsCmd)
}
