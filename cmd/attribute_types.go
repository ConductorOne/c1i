package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

const attributeTypesPath = "/api/v1/user-attribute-types"

var attributeTypesCmd = &cobra.Command{
	Use:   "attribute-types",
	Short: "List the user attribute types a mapping can target",
}

// attrConfigKinds maps the config arm an attribute type (or a mapping's
// attributeType) has set to the config_kind c1i prints.
var attrConfigKinds = []struct{ apiKey, kind string }{
	{"app", "app"},
	{"appUserProfile", "app_user_profile"},
	{"customAppUserProfile", "custom_app_user_profile"},
	{"celExpression", "cel_expression"},
}

// attrConfigKind reports which config arm an attribute type has set; "" for
// none.
func attrConfigKind(at map[string]any) (apiKey, kind string) {
	for _, k := range attrConfigKinds {
		if v, ok := at[k.apiKey].(map[string]any); ok && v != nil {
			return k.apiKey, k.kind
		}
	}
	return "", ""
}

func attributeTypeRow(item map[string]any) map[string]any {
	_, kind := attrConfigKind(item)
	return map[string]any{
		"id":           optString(item, "id"),
		"display_name": optString(item, "displayName"),
		"config_kind":  nilIfEmpty(kind),
	}
}

var attributeTypesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the tenant's user attribute types (NDJSON output)",
	Long: `List the user attribute types: the targets a mapping writes to ("Department",
"Job Title", "Manager Email", ...). Rows are id, display_name and
config_kind: "app" (a type read from the app itself, e.g. Manager Email),
"app_user_profile" (read from one profile key of the app's users), or
"custom_app_user_profile" (Custom: also names the custom user attribute).

Type ids are specific to a tenant: look a type up by display name, as
"attribute-mappings create --type" does. The endpoint takes no page_size or
page_token and returns the whole set.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return getFixedList(cmd, attributeTypesPath, attributeTypeRow)
	},
}

// attributeType is a type as the create body needs it.
type attributeType struct {
	ID, DisplayName, APIKey, Kind string
}

// fetchAttributeTypes reads the whole type set. A cursor on the answer is a
// truncated set, never accepted: a name that is only on page 2 would read as
// unknown.
func fetchAttributeTypes(ctx context.Context, c *client.Client) ([]attributeType, error) {
	data, err := c.Get(ctx, attributeTypesPath, nil)
	if err != nil {
		return nil, fmt.Errorf("listing attribute types: %w", err)
	}
	items, next, err := extractListAndToken(data, "list")
	if err != nil {
		return nil, &nonJSONResponseError{fmt.Errorf("listing attribute types: %w", err)}
	}
	if next != "" {
		return nil, &stuckPaginationCursorError{route: "GET " + attributeTypesPath, reason: "the server reported more pages of a list that has none"}
	}
	var out []attributeType
	for _, raw := range items {
		var item map[string]any
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, &nonJSONResponseError{fmt.Errorf("listing attribute types: %w", err)}
		}
		t := attributeType{ID: strField(item, "id"), DisplayName: strField(item, "displayName")}
		t.APIKey, t.Kind = attrConfigKind(item)
		out = append(out, t)
	}
	return out, nil
}

func strField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// resolveAttributeType finds want among types by exact id, else by exact
// display name. No match is a missing reference (exit 4); a name shared by
// several types is ambiguous (exit 2, ids listed).
func resolveAttributeType(types []attributeType, want string) (attributeType, error) {
	for _, t := range types {
		if t.ID == want {
			return t, nil
		}
	}
	var named []attributeType
	for _, t := range types {
		if t.DisplayName == want {
			named = append(named, t)
		}
	}
	switch len(named) {
	case 1:
		return named[0], nil
	case 0:
		return attributeType{}, refNotFound("type", want, "attribute type (see \"c1i attribute-types list\")",
			&client.APIError{Method: http.MethodGet, Path: attributeTypesPath, StatusCode: http.StatusNotFound})
	}
	ids := make([]string, len(named))
	for i, t := range named {
		ids[i] = t.ID
	}
	return attributeType{}, &usageError{fmt.Errorf("--type %q is ambiguous: %d attribute types have that name; pass one id: %s", want, len(named), strings.Join(ids, ", "))}
}

func init() {
	addLimitFlag(attributeTypesListCmd)
	attributeTypesCmd.AddCommand(attributeTypesListCmd)
	rootCmd.AddCommand(attributeTypesCmd)
}
