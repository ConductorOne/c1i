package cmd

import (
	"context"
	"fmt"
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var attributeMappingsUpdateCmd = &cobra.Command{
	Use:   "update <mapping-id>",
	Short: "Update a user-attribute mapping (pretty JSON)",
	Long: `Change the profile key a mapping reads (--key, and --custom-key for a Custom
type) or its fallback sources (--fallbacks-file).

The API's PUT replaces the mapping: it requires the whole attributeType and
clears fallbacks and profileTypeIds that are left out, and it ignores
updateMask (any path is accepted, none narrows the write). So c1i first reads
the mapping and sends back its current attributeType, fallbacks and
profileTypeIds with only your flags changed, and still sends the mask it
builds (attributeType, fallbacks) as the API documents. --dry-run does the
read too. The read and the write are not atomic: an edit to the fallbacks or
profile types made between them is overwritten. The type itself and the app
cannot be changed; the type's config kind decides which key flags apply (--key
on an app-level type exits 2).

--fallbacks-file is a JSON array replacing the fallbacks; "[]" clears them.
A changed --key reached the mapped users within a minute in the run measured.

Prints the mapping unwrapped like "attribute-mappings get": the id is .id.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		var fallbacks []any
		if flagGiven(cmd, "fallbacks-file") {
			fb, err := readJSONFile[[]any](cmd, "fallbacks-file", mustGetString(cmd, "fallbacks-file"))
			if err != nil {
				return err
			}
			fallbacks = fb
		}
		spec := attributeMappingUpdateSpecFor(fallbacks)
		changed := false
		for _, n := range spec.flagNames() {
			changed = changed || flagGiven(cmd, n)
		}
		if !changed {
			return spec.nothingToUpdate()
		}
		path := attributeMappingPath(id)
		return sendMutation(cmd, mutation{Method: http.MethodPut, Path: path},
			withPreRead(preRead{Run: func(ctx context.Context, c *client.Client, m *mutation) ([]resolvedID, error) {
				body, err := buildAttributeMappingUpdate(ctx, cmd, c, path, spec)
				if err != nil {
					return nil, err
				}
				m.Body = body
				return nil, nil
			}}))
	},
}

// attributeMappingUpdateSpec is the table for flag names and registration; a
// run builds from attributeMappingUpdateSpecFor, whose fallbacks field closes
// over the file read once up front (a "-" file cannot be read twice).
var attributeMappingUpdateSpec = attributeMappingUpdateSpecFor(nil)

func attributeMappingUpdateSpecFor(fallbacks []any) *updateSpec {
	return &updateSpec{fields: []updateField{
		hookUpdate([]string{"key", "custom-key"}, func(cmd *cobra.Command, obj map[string]any) ([]string, error) {
			if !flagGiven(cmd, "key") && !flagGiven(cmd, "custom-key") {
				return nil, nil
			}
			at, _ := obj["attributeType"].(map[string]any)
			apiKey, kind := attrConfigKind(at)
			name, _ := at["displayName"].(string)
			cfg, _ := at[apiKey].(map[string]any)
			if flagGiven(cmd, "custom-key") && kind != "custom_app_user_profile" {
				return nil, &usageError{fmt.Errorf("--custom-key applies only to a custom_app_user_profile mapping; %q is %q", name, kind)}
			}
			if flagGiven(cmd, "key") {
				if kind != "app_user_profile" && kind != "custom_app_user_profile" {
					return nil, &usageError{fmt.Errorf("%q is an app-level attribute (config kind %q): it has no --key to change", name, kind)}
				}
				cfg["appUserProfileAttributeKey"] = mustGetString(cmd, "key")
			}
			if flagGiven(cmd, "custom-key") {
				cfg["userProfileAttributeKey"] = mustGetString(cmd, "custom-key")
			}
			return []string{"attributeType"}, nil
		}),
		hookUpdate([]string{"fallbacks-file"}, func(cmd *cobra.Command, obj map[string]any) ([]string, error) {
			if !flagGiven(cmd, "fallbacks-file") {
				return nil, nil
			}
			obj["fallbacks"] = fallbacks
			return []string{"fallbacks"}, nil
		}),
	}}
}

// buildAttributeMappingUpdate reads the mapping and returns the PUT body: its
// current attributeType, fallbacks and profileTypeIds with the flags applied.
func buildAttributeMappingUpdate(ctx context.Context, cmd *cobra.Command, c *client.Client, path string, spec *updateSpec) (map[string]any, error) {
	data, err := c.Get(ctx, path, nil)
	if err != nil {
		return nil, fmt.Errorf("reading the mapping before updating it: %w", err)
	}
	cur, err := attributeMappingView(data)
	if err != nil {
		return nil, err
	}
	at, ok := cur["attributeType"].(map[string]any)
	if !ok {
		return nil, &nonJSONResponseError{fmt.Errorf("the mapping has no attributeType")}
	}
	obj := map[string]any{"attributeType": at, "fallbacks": orEmptyList(cur["fallbacks"]), "profileTypeIds": orEmptyList(cur["profileTypeIds"])}
	mask, err := spec.buildUpdateMask(cmd, obj)
	if err != nil {
		return nil, err
	}
	setUpdateMask(obj, mask)
	return obj, nil
}

// orEmptyList turns a null or absent list into [], which the PUT reads as the
// value to store.
func orEmptyList(v any) any {
	if v == nil {
		return []any{}
	}
	return v
}

func init() {
	f := attributeMappingsUpdateCmd.Flags()
	f.String("key", "", "New profile key to read (omit to leave unchanged)")
	f.String("custom-key", "", "New custom user attribute to write; custom_app_user_profile mappings only (omit to leave unchanged)")
	f.String("fallbacks-file", "", "JSON array replacing the fallback sources; [] clears them (file, or \"-\" for stdin)")
	registerUpdateSpec(attributeMappingsUpdateCmd, attributeMappingUpdateSpec)
	markMutating(attributeMappingsUpdateCmd)
	attributeMappingsCmd.AddCommand(attributeMappingsUpdateCmd)
}
