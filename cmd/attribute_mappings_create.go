package cmd

import (
	"context"
	"fmt"
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var attributeMappingsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a user-attribute mapping (pretty JSON)",
	Long: `Create a mapping from an app's synced users onto a C1.ai user field.

  c1i attribute-mappings create --app-id <app-id> --type "Department" --key department
  c1i attribute-mappings create --app-id <app-id> --type "Manager Email"

--type is an attribute type's display name or id ("c1i attribute-types
list"). c1i looks the type up first (--dry-run does too, so it needs the same
credential): no such type exits 4, a name shared by several types exits 2
listing their ids; nothing is sent. The request body follows the type's
config kind, so you never pick it:
  app_user_profile         needs --key, the profile key to read
  app (Manager Email,      takes no --key; --key exits 2. The server would
  Directory Status)        answer a wrong key with a 500
  custom_app_user_profile  needs --key and --custom-key, the name of the
                           custom user attribute that receives the value

--fallbacks-file is a JSON array of fallback sources, sent as given:
[{"appUserProfile":{"appId":"<app-id>","appUserProfileAttributeKey":"alt"}}].
--profile-type-id (repeatable) binds profile types; the server refuses it on
a built-in type ("builtin attributes cannot be bound to profile types"). No
profile type existed to test a successful binding.

The mapping's display_name is set by the server (the type's name, or
--custom-key for Custom, as seen in one live run); there is no flag for it.
A second create for the same app and type is not refused: it makes a second
mapping with its own id, so list before creating in a script. A real run with
an unknown --app-id exits 4 (the server's answer); --dry-run looks the type up
but not the app, so it previews such a request.

--body-file sends a full CreateRequest verbatim (attributeType with its
config, appId, ...), mutually exclusive with every other flag; nothing is
looked up.

Prints the mapping unwrapped like "attribute-mappings get": the id is .id
(use jq -er; --fields is a usage error on a mutation). Evaluation is
asynchronous and nothing signals when users have been updated. Precedence
across apps that map the same type was not tested.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		in, err := readAttributeMappingCreateFlags(cmd)
		if err != nil {
			return &usageError{err}
		}
		m := mutation{Method: http.MethodPost, Path: attributeMappingsPath}
		if in.body != nil {
			m.Body = in.body
			return sendMutation(cmd, m)
		}
		return sendMutation(cmd, m, withPreRead(preRead{Run: func(ctx context.Context, c *client.Client, m *mutation) ([]resolvedID, error) {
			body, err := in.requestBody(ctx, c)
			if err != nil {
				return nil, err
			}
			m.Body = body
			return nil, nil
		}}))
	},
}

// attributeMappingCreate is the parsed flag set of create: the verbatim body
// of --body-file, or the inputs build combines with the looked-up type.
type attributeMappingCreate struct {
	body           map[string]any // --body-file; nil otherwise
	appID, typ     string
	key, customKey string
	fallbacks      []any
	profileTypeIDs []string
}

var attributeMappingCreateFlagNames = []string{"app-id", "type", "key", "custom-key", "fallbacks-file", "profile-type-id"}

func readAttributeMappingCreateFlags(cmd *cobra.Command) (*attributeMappingCreate, error) {
	if flagGiven(cmd, "body-file") {
		for _, n := range attributeMappingCreateFlagNames {
			if flagGiven(cmd, n) {
				return nil, &usageError{fmt.Errorf("--body-file is mutually exclusive with --%s", n)}
			}
		}
		body, err := readJSONFile[map[string]any](cmd, "body-file", mustGetString(cmd, "body-file"))
		if err != nil {
			return nil, err
		}
		return &attributeMappingCreate{body: body}, nil
	}
	if err := requireNonEmpty(cmd, "app-id", "type"); err != nil {
		return nil, fmt.Errorf("%w (or pass --body-file)", err)
	}
	in := &attributeMappingCreate{
		appID:     mustGetString(cmd, "app-id"),
		typ:       mustGetString(cmd, "type"),
		key:       mustGetString(cmd, "key"),
		customKey: mustGetString(cmd, "custom-key"),
	}
	if flagGiven(cmd, "fallbacks-file") {
		fb, err := readJSONFile[[]any](cmd, "fallbacks-file", mustGetString(cmd, "fallbacks-file"))
		if err != nil {
			return nil, err
		}
		in.fallbacks = fb
	}
	ids, err := repeatableStringFlag(cmd, "profile-type-id")
	if err != nil {
		return nil, err
	}
	in.profileTypeIDs = ids
	return in, nil
}

// checkAttributeKeys refuses a --key / --custom-key the type's config kind does not
// take, and requires the ones it does.
func checkAttributeKeys(kind, typeName, key, customKey string) error {
	switch kind {
	case "app":
		if key != "" || customKey != "" {
			return &usageError{fmt.Errorf("%s is an app-level attribute (config kind app): it takes no --key or --custom-key", typeName)}
		}
	case "app_user_profile":
		if key == "" {
			return &usageError{fmt.Errorf("%s reads one profile key of the app's users (config kind app_user_profile): --key is required", typeName)}
		}
		if customKey != "" {
			return &usageError{fmt.Errorf("--custom-key applies only to a custom_app_user_profile type; %s is %s", typeName, kind)}
		}
	case "custom_app_user_profile":
		if key == "" || customKey == "" {
			return &usageError{fmt.Errorf("%s (config kind custom_app_user_profile) requires --key (the profile key to read) and --custom-key (the custom user attribute to write)", typeName)}
		}
	default:
		return &usageError{fmt.Errorf("%s has config kind %q, which c1i does not build; pass the full request with --body-file", typeName, kind)}
	}
	return nil
}

// requestBody looks the type up and assembles the request body from its config kind.
func (in *attributeMappingCreate) requestBody(ctx context.Context, c *client.Client) (map[string]any, error) {
	types, err := fetchAttributeTypes(ctx, c)
	if err != nil {
		return nil, err
	}
	t, err := resolveAttributeType(types, in.typ)
	if err != nil {
		return nil, err
	}
	if err := checkAttributeKeys(t.Kind, t.DisplayName, in.key, in.customKey); err != nil {
		return nil, err
	}
	cfg := map[string]any{"appId": in.appID}
	body := map[string]any{"appId": in.appID}
	if in.key != "" {
		cfg["appUserProfileAttributeKey"] = in.key
		body["appUserProfileAttributeKey"] = in.key
	}
	if in.customKey != "" {
		cfg["userProfileAttributeKey"] = in.customKey
	}
	body["attributeType"] = map[string]any{"id": t.ID, "displayName": t.DisplayName, t.APIKey: cfg}
	if in.fallbacks != nil {
		body["fallbacks"] = in.fallbacks
	}
	if len(in.profileTypeIDs) > 0 {
		body["profileTypeIds"] = in.profileTypeIDs
	}
	return body, nil
}

func init() {
	f := attributeMappingsCreateCmd.Flags()
	f.String("app-id", "", "App whose users the mapping reads (required unless --body-file)")
	f.String("type", "", "Attribute type display name or id (required unless --body-file)")
	f.String("key", "", "Profile key to read; required for app_user_profile and custom types, refused for app types")
	f.String("custom-key", "", "Custom user attribute to write; custom_app_user_profile types only")
	f.String("fallbacks-file", "", "JSON array of fallback sources (file, or \"-\" for stdin)")
	addRepeatableStringFlag(attributeMappingsCreateCmd, "profile-type-id", "Profile type id to bind (repeatable)")
	f.String("body-file", "", "Full CreateRequest JSON object, sent verbatim (file, or \"-\" for stdin); mutually exclusive with the flags above")
	annotateRequired(attributeMappingsCreateCmd, "app-id", "type")
	markMutating(attributeMappingsCreateCmd)
	attributeMappingsCmd.AddCommand(attributeMappingsCreateCmd)
}
