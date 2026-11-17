package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

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
		appID, _ := cmd.Flags().GetString("app-id")
		pageSize := pageSizeFlag(cmd)
		return listJSONPages(cmd, "GET "+attributeMappingsPath, []string{"id"}, attributeMappingRow,
			func(c *client.Client, size int, token string) ([]byte, error) {
				// With a filter a page can shrink to nothing, so the page size
				// must not follow --limit down.
				if appID != "" {
					size = pageSize
				}
				params := map[string]string{"page_size": strconv.Itoa(size)}
				if token != "" {
					params["page_token"] = token
				}
				data, err := c.Get(cmd.Context(), attributeMappingsPath, params)
				if err != nil || appID == "" {
					return data, err
				}
				return keepMappingsOfApp(data, appID)
			})
	},
}

// keepMappingsOfApp drops the list items of other apps from one page, leaving
// every other key (nextPageToken, expanded) as the server sent it.
func keepMappingsOfApp(data []byte, appID string) ([]byte, error) {
	var page map[string]json.RawMessage
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, &nonJSONResponseError{err}
	}
	var items []json.RawMessage
	if raw, ok := page["list"]; ok {
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, &nonJSONResponseError{err}
		}
	}
	kept := make([]json.RawMessage, 0, len(items))
	for _, raw := range items {
		var item struct {
			Mapping struct {
				AppID string `json:"appId"`
			} `json:"userAttributeMapping"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, &nonJSONResponseError{err}
		}
		if item.Mapping.AppID == appID {
			kept = append(kept, raw)
		}
	}
	page["list"], _ = json.Marshal(kept)
	return json.Marshal(page)
}

var attributeMappingsGetCmd = &cobra.Command{
	Use:   "get <mapping-id>",
	Short: "Get a single user-attribute mapping (pretty JSON)",
	Long: `Get one mapping, unwrapped: the id is .id, the type's config is
.attributeType, and appPath and fallbacksPath stay as siblings.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}
		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Get(cmd.Context(), attributeMappingPath(args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeResource(cmd, data, "id")
	},
}

func attributeMappingPath(id string) string { return client.Path(attributeMappingsPath+"/%s", id) }

func init() {
	attributeMappingsListCmd.Flags().String("app-id", "", "Only list mappings of this app")
	addPaginationFlags(attributeMappingsListCmd)
	attributeMappingsCmd.AddCommand(attributeMappingsListCmd, attributeMappingsGetCmd)
	rootCmd.AddCommand(attributeMappingsCmd)
}
