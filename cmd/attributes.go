package cmd

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

const (
	attributeValuesPath = "/api/v1/attributes"
	attributeTypesRoute = "/api/v1/attributes/types"
)

var attributesCmd = &cobra.Command{
	Use:   "attributes",
	Short: "List, search, create and delete tenant attribute values (Compliance Framework, Risk Level)",
	Long: `Tenant attribute values: the values of the tenant's attribute types, such as
the Compliance Framework values (SOC2, HIPAA, ...) and Risk Level values
(Low, High, ...). This is a different resource from "attribute-types" and
"attribute-mappings", which map an app's profile keys onto C1.ai user fields.

Run "attributes types" to see the types, then "attributes list --type" for a
type's values. A value has no update: delete it and create another.

The API also has per-type routes (/api/v1/attributes/compliance_frameworks,
/api/v1/attributes/risk_levels, ...). They are the same operations restricted
to one type, so they have no commands here; use "c1i api" for them.`,
}

// attributeValueType is an attribute type as the value commands need it.
type attributeValueType struct{ ID, Name string }

// fetchAttributeValueTypes reads every attribute type, following its cursor.
func fetchAttributeValueTypes(ctx context.Context, c *client.Client) ([]attributeValueType, error) {
	items, err := collectPages(c, pageSpec[map[string]any]{
		Route: "GET " + attributeTypesRoute,
		Fetch: getPage(ctx, attributeTypesRoute, nil),
		Parse: parseList[map[string]any],
		ID:    func(item map[string]any) string { return optStr(item, "id") },
	})
	if err != nil {
		return nil, err
	}
	out := make([]attributeValueType, 0, len(items))
	for _, item := range items {
		out = append(out, attributeValueType{ID: optStr(item, "id"), Name: optStr(item, "name")})
	}
	return out, nil
}

// resolveAttributeValueType finds want among types by exact id, else by exact
// name. No match is a missing reference (exit 4); a name shared by several
// types is ambiguous (exit 2, ids listed).
func resolveAttributeValueType(flag string, types []attributeValueType, want string) (attributeValueType, error) {
	for _, t := range types {
		if t.ID == want {
			return t, nil
		}
	}
	var named []attributeValueType
	for _, t := range types {
		if t.Name == want {
			named = append(named, t)
		}
	}
	switch len(named) {
	case 1:
		return named[0], nil
	case 0:
		return attributeValueType{}, refNotFoundLocal(flag, want, "attribute type (see \"c1i attributes types\")")
	}
	ids := make([]string, len(named))
	for i, t := range named {
		ids[i] = t.ID
	}
	return attributeValueType{}, &usageError{fmt.Errorf("--%s %q is ambiguous: %d attribute types have that name; pass one id: %s", flag, want, len(named), strings.Join(ids, ", "))}
}

func attributeTypeValueRow(item map[string]any) map[string]any {
	return map[string]any{"id": optString(item, "id"), "name": optString(item, "name")}
}

func attributeValueRow(item map[string]any) map[string]any {
	return map[string]any{
		"id":         optString(item, "id"),
		"type_id":    optString(item, "attributeTypeId"),
		"value":      optString(item, "value"),
		"created_at": optString(item, "createdAt"),
		"updated_at": optString(item, "updatedAt"),
		"deleted_at": optString(item, "deletedAt"),
	}
}

var attributesTypesCmd = &cobra.Command{
	Use:   "types",
	Short: "List the attribute types that have values (NDJSON output)",
	Long:  "List the tenant's attribute types. Rows are id and name.",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		return listPages(cmd, c, pageSpec[map[string]any]{
			Route: "GET " + attributeTypesRoute,
			Fetch: getPage(cmd.Context(), attributeTypesRoute, nil),
			Parse: parseList[map[string]any],
			ID:    func(item map[string]any) string { return optStr(item, "id") },
			Rows:  rowFn(attributeTypeValueRow),
		})
	},
}

var attributesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the values of one attribute type (NDJSON output)",
	Long: `List the values of one attribute type. --type takes the type's name or id
(see "attributes types"); an unknown type exits 4 before the list is sent.
Rows are id, type_id, value, created_at, updated_at and deleted_at (null for
a live value).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		t, err := lookupAttributeValueType(cmd, c, "type")
		if err != nil {
			return err
		}
		path := client.Path(attributeTypesRoute+"/%s/values", t.ID)
		return listPages(cmd, c, pageSpec[map[string]any]{
			Route: "GET " + attributeTypesRoute + "/{attribute_type_id}/values",
			Fetch: getPage(cmd.Context(), path, nil),
			Parse: parseList[map[string]any],
			ID:    func(item map[string]any) string { return optStr(item, "id") },
			Rows:  rowFn(attributeValueRow),
		})
	},
}

// lookupAttributeValueType resolves the single --flag against the type list.
func lookupAttributeValueType(cmd *cobra.Command, c *client.Client, flag string) (attributeValueType, error) {
	types, err := fetchAttributeValueTypes(cmd.Context(), c)
	if err != nil {
		return attributeValueType{}, err
	}
	return resolveAttributeValueType(flag, types, mustGetString(cmd, flag))
}

var attributesSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search attribute values by type, id, value or text (NDJSON output)",
	Long: `Search attribute values. --type (a name or id, repeatable) keeps values of
those types; each is looked up first and an unknown one exits 4, because the
server answers an unknown type id with an empty list. --id and --exclude-id
(repeatable) include or remove value ids, --value is the server's
case-insensitive match on the value, and --query its fuzzy match. Rows are the
same shape "attributes list" emits.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		typeArgs, err := repeatableStringFlag(cmd, "type")
		if err != nil {
			return err
		}
		ids, err := repeatableStringFlag(cmd, "id")
		if err != nil {
			return err
		}
		excludeIDs, err := repeatableStringFlag(cmd, "exclude-id")
		if err != nil {
			return err
		}
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		body := map[string]any{}
		if len(typeArgs) > 0 {
			types, err := fetchAttributeValueTypes(cmd.Context(), c)
			if err != nil {
				return err
			}
			var typeIDs []string
			for _, want := range dedupe(typeArgs) {
				t, err := resolveAttributeValueType("type", types, want)
				if err != nil {
					return err
				}
				typeIDs = append(typeIDs, t.ID)
			}
			body["attributeTypeIds"] = dedupe(typeIDs)
		}
		if len(ids) > 0 {
			body["ids"] = dedupe(ids)
		}
		if len(excludeIDs) > 0 {
			body["excludeIds"] = dedupe(excludeIDs)
		}
		if v, _ := cmd.Flags().GetString("value"); v != "" {
			body["value"] = v
		}
		if q, _ := cmd.Flags().GetString("query"); q != "" {
			body["query"] = q
		}
		return listPages(cmd, c, pageSpec[map[string]any]{
			Route: "POST /api/v1/search/attributes",
			Fetch: postPage(cmd.Context(), "/api/v1/search/attributes", body),
			Parse: parseList[map[string]any],
			ID:    func(item map[string]any) string { return optStr(item, "id") },
			Rows:  rowFn(attributeValueRow),
		})
	},
}

var attributesGetCmd = &cobra.Command{
	Use:   "get <value-id>",
	Short: "Get one attribute value (pretty JSON)",
	Long: `Get one attribute value by id. A soft-deleted value is still returned, with
deleted_at set.`,
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
		data, err := c.Get(cmd.Context(), client.Path(attributeValuesPath+"/%s", args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeResource(cmd, data, "id")
	},
}

var attributesCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an attribute value",
	Long: `Create a value under an attribute type. --type takes the type's name or id
and is looked up first (also on --dry-run): an unknown type exits 4, because
the server would otherwise create a value under it anyway. The server accepts
a repeated value, including one differing only in case, as a new value with
its own id. It rejects an empty value and, per its own error, one over 512
bytes ("value length must be between 1 and 512 bytes, inclusive").

Prints the new value, unwrapped like "attributes get". Honors --dry-run.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		value := mustGetString(cmd, "value")
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: attributeValuesPath, Unwrap: named("value")},
			withPreRead(preRead{Run: func(ctx context.Context, c *client.Client, m *mutation) ([]resolvedID, error) {
				types, err := fetchAttributeValueTypes(ctx, c)
				if err != nil {
					return nil, err
				}
				t, err := resolveAttributeValueType("type", types, mustGetString(cmd, "type"))
				if err != nil {
					return nil, err
				}
				m.Body = map[string]any{"attributeTypeId": t.ID, "value": value}
				return nil, nil
			}}))
	},
}

var attributesDeleteCmd = &cobra.Command{
	Use:   "delete <value-id>",
	Short: "Soft-delete an attribute value",
	Long: `Soft-delete an attribute value by id. "attributes list" and "attributes
search" stop returning it, while "attributes get" still does, with deleted_at
set. Deleting it again exits 0. An unknown id exits 4.

Prints the API's response, which is {}; exit 0 is the success signal.
Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: client.Path("/api/v1/attribute/%s", args[0])})
	},
}

func init() {
	addPaginationFlags(attributesTypesCmd)
	attributesListCmd.Flags().String("type", "", "Attribute type name or id")
	markRequired(attributesListCmd, "type")
	addPaginationFlags(attributesListCmd)
	addRepeatableStringFlag(attributesSearchCmd, "type", "Only values of this attribute type, by name or id (repeatable)")
	addRepeatableStringFlag(attributesSearchCmd, "id", "Only this value id (repeatable)")
	addRepeatableStringFlag(attributesSearchCmd, "exclude-id", "Leave out this value id (repeatable)")
	attributesSearchCmd.Flags().String("value", "", "Case-insensitive match on the value")
	attributesSearchCmd.Flags().String("query", "", "Fuzzy search on the value")
	addPaginationFlags(attributesSearchCmd)
	attributesCreateCmd.Flags().String("type", "", "Attribute type name or id")
	attributesCreateCmd.Flags().String("value", "", "The value to create")
	markRequired(attributesCreateCmd, "type", "value")
	markMutating(attributesCreateCmd)
	markMutating(attributesDeleteCmd)
	attributesCmd.AddCommand(attributesTypesCmd, attributesListCmd, attributesSearchCmd, attributesGetCmd, attributesCreateCmd, attributesDeleteCmd)
	rootCmd.AddCommand(attributesCmd)
}
