package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

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
		if err := requireNonEmpty(cmd, "app-id"); err != nil {
			return err
		}
		refs, err := catalogEntitlementReferences(cmd, true)
		if err != nil {
			return err
		}
		body := buildRequestableEntitlementsBody(cmd, refs)
		path := client.Path("/api/v1/catalogs/%s/requestable_entries", args[0])
		return postAccessProfileEntitlements(cmd, path, body)
	},
}

var accessProfilesRequestableEntitlementsRemoveCmd = &cobra.Command{
	Use:   "remove <access-profile-id>",
	Short: "Remove requestable entitlements from an access profile (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id"); err != nil {
			return err
		}
		refs, err := catalogEntitlementReferences(cmd, true)
		if err != nil {
			return err
		}
		path := client.Path("/api/v1/catalogs/%s/requestable_entries", args[0])
		return deleteAccessProfileEntitlements(cmd, path, buildRequestableEntitlementsBody(cmd, refs))
	},
}

var accessProfilesRequestableEntitlementsSetCmd = &cobra.Command{
	Use:   "set <access-profile-id>",
	Short: "Replace all requestable entitlements (pretty JSON)",
	Long: `Replace the full requestable-entitlement set in an access profile.

Pass --refs-file with a JSON array of {"appId":"…","id":"…"} objects. Every
existing requestable entitlement not in that array is removed; pass [] to
clear the entire set.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		refs, err := catalogEntitlementReferencesFromFile(cmd)
		if err != nil {
			return err
		}
		body := buildRequestableEntitlementsBody(cmd, refs)
		path := client.Path("/api/v1/catalogs/%s/requestable_entitlements/update", args[0])
		return postAccessProfileEntitlements(cmd, path, body)
	},
}

var accessProfilesVisibilityEntitlementsAddCmd = &cobra.Command{
	Use:   "add <access-profile-id>",
	Short: "Add visibility entitlements to an access profile (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id"); err != nil {
			return err
		}
		refs, err := catalogEntitlementReferences(cmd, true)
		if err != nil {
			return err
		}
		path := client.Path("/api/v1/catalogs/%s/visibility_bindings", args[0])
		return postAccessProfileEntitlements(cmd, path, buildVisibilityEntitlementsBody(refs))
	},
}

var accessProfilesVisibilityEntitlementsRemoveCmd = &cobra.Command{
	Use:   "remove <access-profile-id>",
	Short: "Remove visibility entitlements from an access profile (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id"); err != nil {
			return err
		}
		refs, err := catalogEntitlementReferences(cmd, true)
		if err != nil {
			return err
		}
		path := client.Path("/api/v1/catalogs/%s/visibility_bindings", args[0])
		return deleteAccessProfileEntitlements(cmd, path, buildVisibilityEntitlementsBody(refs))
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
			baseURL, err := GetBaseURL()
			if err != nil {
				return err
			}

			c, err := newListClient(cmd, baseURL)
			if err != nil {
				return fmt.Errorf("authentication failed: %w", err)
			}

			requestedPageSize := pageSizeFlag(cmd)
			pageToken, _ := cmd.Flags().GetString("page-token")
			manualPaging := cmd.Flags().Changed("page-token")
			limit := getIntFlag(cmd, "limit")
			enc := newEmitter(cmd)
			path := client.Path("/api/v1/catalogs/%s/"+endpoint, args[0])

			for !limitReached(enc.Written(), limit) {
				pageSize := requestedPageSize
				if !enc.Filtered() {
					pageSize = effectivePageSize(requestedPageSize, limit, enc.Written())
				}
				params := map[string]string{"page_size": strconv.Itoa(pageSize)}
				if pageToken != "" {
					params["page_token"] = pageToken
				}

				data, err := c.Get(cmd.Context(), path, params)
				if err != nil {
					return fmt.Errorf("API error: %w", err)
				}
				var resp struct {
					List          []entitlementListItem `json:"list"`
					NextPageToken string                `json:"nextPageToken"`
				}
				if err := json.Unmarshal(data, &resp); err != nil {
					return fmt.Errorf("failed to parse response: %w", err)
				}
				for _, item := range resp.List {
					_ = enc.Encode(entitlementRow(item))
					if limitReached(enc.Written(), limit) {
						return nil
					}
				}
				if resp.NextPageToken == "" || manualPaging {
					break
				}
				pageToken = resp.NextPageToken
			}
			return nil
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
	refs, err := readJSONArrayFile(cmd, refsFile)
	if err != nil {
		return nil, fmt.Errorf("reading --refs-file: %w", err)
	}
	if refs == nil {
		return nil, &usageError{fmt.Errorf("--refs-file must contain a JSON array")}
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

func validateAppEntitlementRefs(refs []any, field string) error {
	for i, value := range refs {
		ref, ok := value.(map[string]any)
		if !ok || len(ref) != 2 {
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

func postAccessProfileEntitlements(cmd *cobra.Command, path string, body map[string]any) error {
	if dryRunActive() {
		return printDryRun(cmd, http.MethodPost, path, body)
	}
	baseURL, err := GetBaseURL()
	if err != nil {
		return err
	}
	c, err := newClient(cmd, baseURL)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	data, err := c.Post(cmd.Context(), path, body)
	if err != nil {
		return fmt.Errorf("API error: %w", err)
	}
	return writeRawObject(cmd, data)
}

func deleteAccessProfileEntitlements(cmd *cobra.Command, path string, body map[string]any) error {
	if dryRunActive() {
		return printDryRun(cmd, http.MethodDelete, path, body)
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return err
	}
	baseURL, err := GetBaseURL()
	if err != nil {
		return err
	}
	c, err := newClient(cmd, baseURL)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	data, err := c.Request(cmd.Context(), http.MethodDelete, path, bodyBytes, nil)
	if err != nil {
		return fmt.Errorf("API error: %w", err)
	}
	return writeRawObject(cmd, data)
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
	}
	accessProfilesRequestableEntitlementsSetCmd.Flags().String("refs-file", "", "JSON array of AppEntitlementRef objects (file, or \"-\" for stdin)")
	accessProfilesRequestableEntitlementsAddCmd.Flags().Bool("create-requests", false, "Create requests for users in the access profile when adding these entitlements")

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
