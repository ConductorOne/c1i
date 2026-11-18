package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

const attributeMappingsPath = "/api/v1/user-attribute-mappings"

var attributeMappingsCmd = &cobra.Command{
	Use:   "attribute-mappings",
	Short: "Map an app's profile attributes onto C1.ai user fields",
	Long: `Manage user-attribute mappings: each one copies a value from an app's synced
users (a profile key, or the app's manager email) into a structured C1.ai
user field such as department, job title, employment type or manager.

Values are copied verbatim ("Full-Time" stays "Full-Time"), so a CEL
comparison against a lowercase literal will not match. Evaluation runs
asynchronously and no response signals completion: re-read the user.
Attribute type ids are specific to a tenant; pass --type by display name
(see "c1i attribute-types list").

Precedence when two apps map the same attribute type was not tested: do not
add a mapping for a type another app already maps on users that overlap.`,
}

// attributeMappingView returns the mapping inside a response envelope
// ({"attribute":{"userAttributeMapping":{...}}} for create/get/update).
func attributeMappingView(data []byte) (map[string]any, error) {
	var env struct {
		Attribute struct {
			Mapping map[string]any `json:"userAttributeMapping"`
		} `json:"attribute"`
	}
	if err := json.Unmarshal(data, &env); err != nil || env.Attribute.Mapping == nil {
		return nil, &nonJSONResponseError{fmt.Errorf("the response held no attribute.userAttributeMapping")}
	}
	return env.Attribute.Mapping, nil
}

// attributeMappingRow flattens one list item ({userAttributeMapping, appPath,
// fallbacksPath}) into a row. Numbers stay numbers; an absent key (an app-level
// type has none) is null, not "".
func attributeMappingRow(item map[string]any) map[string]any {
	m, _ := item["userAttributeMapping"].(map[string]any)
	at, _ := m["attributeType"].(map[string]any)
	_, kind := attrConfigKind(at)
	return map[string]any{
		"id":                     optString(m, "id"),
		"app_id":                 optString(m, "appId"),
		"key":                    optString(m, "appUserProfileAttributeKey"),
		"display_name":           optString(m, "displayName"),
		"attribute_type_id":      optString(at, "id"),
		"attribute_type_name":    optString(at, "displayName"),
		"config_kind":            nilIfEmpty(kind),
		"priority":               m["priority"],
		"count":                  m["count"],
		"evaluation_error_count": m["evaluationErrorCount"],
	}
}

var attributeMappingsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List user-attribute mappings (NDJSON output)",
	Long: `List user-attribute mappings, auto-paginated. Rows: id, app_id, key,
display_name, attribute_type_id, attribute_type_name, config_kind (see
"attribute-types list"), priority, count, evaluation_error_count. key is null
for an app-level type such as Manager Email.

--app-id keeps only that app's mappings. The endpoint has no app filter, so it
is applied after each page is read and every page is still fetched.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		appID, _ := cmd.Flags().GetString("app-id")
		return listPages(cmd, c, pageSpec[map[string]any]{
			Route: "GET " + attributeMappingsPath,
			Fetch: getPage(cmd.Context(), attributeMappingsPath, nil),
			Parse: func(data []byte) ([]map[string]any, string, error) {
				items, next, err := parseList[map[string]any](data)
				if err != nil || appID == "" {
					return items, next, err
				}
				items, err = keepMappingsOfApp(items, appID)
				return items, next, err
			},
			ID:   func(item map[string]any) string { return optStr(attributeMappingOf(item), "id") },
			Rows: rowFn(attributeMappingRow),
			// A filtered page can shrink to nothing, so the page size must not
			// follow --limit down.
			FixedSize: appID != "",
		})
	},
}

func attributeMappingOf(item map[string]any) map[string]any {
	m, _ := item["userAttributeMapping"].(map[string]any)
	return m
}

func optStr(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// keepMappingsOfApp drops the items of other apps from one page. An item with
// no appId is a shape change, not "another app's": it fails rather than
// reading as an empty result.
func keepMappingsOfApp(items []map[string]any, appID string) ([]map[string]any, error) {
	kept := make([]map[string]any, 0, len(items))
	for _, item := range items {
		got := optStr(attributeMappingOf(item), "appId")
		if got == "" {
			return nil, &nonJSONResponseError{fmt.Errorf("a list item has no userAttributeMapping.appId, so --app-id cannot be applied")}
		}
		if got == appID {
			kept = append(kept, item)
		}
	}
	return kept, nil
}

var attributeMappingsGetCmd = &cobra.Command{
	Use:   "get <mapping-id>",
	Short: "Get a single user-attribute mapping (pretty JSON)",
	Long: `Get one mapping, unwrapped: the id is .id, the type's config is
.attributeType, and appPath and fallbacksPath stay as siblings.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return getResource(cmd, attributeMappingPath(args[0]), nil, "id", "attribute", "userAttributeMapping")
	},
}

func attributeMappingPath(id string) string { return client.Path(attributeMappingsPath+"/%s", id) }

func init() {
	attributeMappingsListCmd.Flags().String("app-id", "", "Only list mappings of this app")
	addPaginationFlags(attributeMappingsListCmd)
	attributeMappingsCmd.AddCommand(attributeMappingsListCmd, attributeMappingsGetCmd)
	rootCmd.AddCommand(attributeMappingsCmd)
}
