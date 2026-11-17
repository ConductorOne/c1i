package cmd

import (
	"fmt"
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// catalogEntitlementRef is the API's AppEntitlementRef: an entitlement ID is
// scoped to its application, so neither field can be omitted from a mutation.
type catalogEntitlementRef struct {
	AppID string `json:"appId"`
	ID    string `json:"id"`
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
	Short: "Add requestable entitlements to an access profile (pretty JSON)",
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
	Short: "Remove requestable entitlements from an access profile (pretty JSON)",
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
	Short: "Replace all requestable entitlements (pretty JSON)",
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
	Short: "Add visibility entitlements to an access profile (pretty JSON)",
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
	Short: "Remove visibility entitlements from an access profile (pretty JSON)",
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

func catalogEntitlementReferences(cmd *cobra.Command, required bool) ([]catalogEntitlementRef, error) {
	ids, err := repeatableStringFlag(cmd, "entitlement-id")
	if err != nil {
		return nil, err
	}
	if required && len(ids) == 0 {
		return nil, &usageError{fmt.Errorf("flag --entitlement-id requires at least one value")}
	}
	appID, _ := cmd.Flags().GetString("app-id")
	refs := make([]catalogEntitlementRef, len(ids))
	for i, id := range ids {
		refs[i] = catalogEntitlementRef{AppID: appID, ID: id}
	}
	return refs, nil
}

func catalogEntitlementReferencesFromFile(cmd *cobra.Command) ([]catalogEntitlementRef, error) {
	refsFile, _ := cmd.Flags().GetString("refs-file")
	if refsFile == "" {
		return nil, &usageError{fmt.Errorf("--refs-file is required")}
	}
	refs, err := readJSONFile[[]any](cmd, "refs-file", refsFile)
	if err != nil {
		return nil, err
	}
	if err := validateAppEntitlementRefs(refs, "refs"); err != nil {
		return nil, err
	}

	result := make([]catalogEntitlementRef, len(refs))
	for i, raw := range refs {
		ref := raw.(map[string]any)
		result[i] = catalogEntitlementRef{AppID: ref["appId"].(string), ID: ref["id"].(string)}
	}
	return result, nil
}

func validateAppEntitlementRefs(raw []any, field string) error {
	refs, err := objectElements(field, raw)
	if err != nil {
		return err
	}
	for i, ref := range refs {
		if len(ref) != 2 {
			return &usageError{fmt.Errorf("%s[%d] must be an AppEntitlementRef with exactly appId and id", field, i)}
		}
		appID, appOK := ref["appId"].(string)
		id, idOK := ref["id"].(string)
		if !appOK || !idOK || appID == "" || id == "" {
			return &usageError{fmt.Errorf("%s[%d] must be an AppEntitlementRef with non-empty appId and id", field, i)}
		}
	}
	return nil
}

func buildRequestableEntitlementsBody(cmd *cobra.Command, refs []catalogEntitlementRef) map[string]any {
	body := map[string]any{"appEntitlements": refs}
	if flag := cmd.Flags().Lookup("create-requests"); flag != nil && flag.Changed {
		createRequests, _ := cmd.Flags().GetBool("create-requests")
		body["createRequests"] = createRequests
	}
	return body
}

func buildVisibilityEntitlementsBody(refs []catalogEntitlementRef) map[string]any {
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
