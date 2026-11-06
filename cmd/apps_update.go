package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var appsUpdateCmd = &cobra.Command{
	Use:   "update <app-id>",
	Short: "Update an app's name, description, policies, or identity matching (pretty JSON)",
	Long: `Update writable app fields.

The API requires {"app": {"id": "...", ...}, "updateMask": "..."}. The update
mask is derived exactly from the flags you pass: an omitted flag is left
alone, while an explicit empty value is a change.

--display-name cannot be empty or whitespace-only (exit 2, nothing sent):
the server answers "" with exit 0 but keeps the old name, and stores "  ".

--grant-policy-id, --revoke-policy-id and --certify-policy-id bind a policy
to the app. Before sending, c1i fetches each policy and refuses (exit 2,
nothing sent) an id that doesn't exist, is deleted, or isn't of the flag's
type (POLICY_TYPE_GRANT, _REVOKE or _CERTIFY). --dry-run skips this check:
it builds no client and sends nothing.

To unbind, pass --unbind-policy grant|revoke|certify (repeatable). An empty
policy id is a usage error rather than an unbind, so an unset shell variable
can't silently remove a policy; setting and unbinding the same kind is too.

--identity-matching accepts strict, display-name, or custom, or the full
APP_USER_IDENTITY_MATCHING_* name of one of them (case-insensitive); anything
else is a usage error (exit 2) before any request is sent.

--app-user-mapper-file takes a JSON object (file, or "-" for stdin) that
REPLACES the app's whole user mapper:
  {"mappingCases": [{"userKeyCel": "subject.email", "appUserKeyCel": "account.email"}]}
userKeyCel runs against the C1.ai user as "subject", appUserKeyCel against
the app user as "account". The server validates the CEL and 400s (exit 2)
with e.g. "invalid app_user_key_cel in mapping case 0: ...". Only the file's
shape is checked here, and mappingCases must be non-empty: the server
accepts an empty mapper with exit 0 but keeps the existing one. This API
offers no way to remove a mapper.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := requireNonEmptyIfSet(cmd, "display-name"); err != nil {
			return err
		}
		body, err := buildAppUpdateBody(cmd, args[0])
		if err != nil {
			return &usageError{err}
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		path := client.Path("/api/v1/apps/%s", args[0])
		if dryRunActive() {
			return printDryRun(cmd, "POST", path, body)
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		if err := validatePolicyBindings(cmd.Context(), cmd, c, appPolicyKinds); err != nil {
			return err
		}
		data, err := c.Post(cmd.Context(), path, body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeRawObject(cmd, data)
	},
}

// appStringFields are the plain string App fields shared by create and update,
// in mask order.
var appStringFields = []struct{ flag, key string }{
	{"display-name", "displayName"},
	{"description", "description"},
}

// UNSPECIFIED is deliberately absent: an update to it exits 0 but leaves the
// current value in place.
var appIdentityMatchingValues = map[string]string{
	"strict":       "APP_USER_IDENTITY_MATCHING_STRICT",
	"display-name": "APP_USER_IDENTITY_MATCHING_DISPLAY_NAME",
	"custom":       "APP_USER_IDENTITY_MATCHING_CUSTOM",
}

// mapIdentityMatching maps --identity-matching to the API enum.
// '-' and '_' are interchangeable, as in mapPolicyType.
func mapIdentityMatching(raw string) (string, error) {
	v := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(raw), "_", "-"))
	if api, ok := appIdentityMatchingValues[v]; ok {
		return api, nil
	}
	full := strings.ToUpper(strings.ReplaceAll(v, "-", "_"))
	for _, api := range appIdentityMatchingValues {
		if full == api {
			return api, nil
		}
	}
	return "", fmt.Errorf("invalid --identity-matching %q: want strict, display-name, custom, or the full APP_USER_IDENTITY_MATCHING_* name of one of them", raw)
}

// readAppUserMapperFile reads --app-user-mapper-file. Unknown keys pass
// through verbatim, like every other --*-file object flag.
func readAppUserMapperFile(cmd *cobra.Command, path string) (map[string]any, error) {
	data, err := readJSONBytes(cmd, path)
	if err != nil {
		return nil, fmt.Errorf("reading --app-user-mapper-file: %w", err)
	}
	var mapper map[string]any
	if err := json.Unmarshal(data, &mapper); err != nil || mapper == nil {
		return nil, fmt.Errorf(`--app-user-mapper-file must be a JSON object like {"mappingCases":[{"userKeyCel":"...","appUserKeyCel":"..."}]}`)
	}
	cases, ok := mapper["mappingCases"].([]any)
	if !ok || len(cases) == 0 {
		// The server accepts an empty mapper with exit 0 and keeps the old one.
		return nil, fmt.Errorf("--app-user-mapper-file: mappingCases must be a non-empty array (an empty mapper does not remove the existing one)")
	}
	for i, c := range cases {
		obj, ok := c.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("--app-user-mapper-file: mappingCases[%d] must be an object", i)
		}
		for _, k := range []string{"userKeyCel", "appUserKeyCel"} {
			if v, present := obj[k]; present {
				if _, isStr := v.(string); !isStr {
					return nil, fmt.Errorf("--app-user-mapper-file: mappingCases[%d].%s must be a string", i, k)
				}
			}
		}
	}
	return mapper, nil
}

// buildAppUpdateBody builds the Update request wrapper from changed flags.
func buildAppUpdateBody(cmd *cobra.Command, id string) (map[string]any, error) {
	app := map[string]any{"id": id}
	var paths []string
	for _, sf := range appStringFields {
		if cmd.Flags().Changed(sf.flag) {
			v, _ := cmd.Flags().GetString(sf.flag)
			app[sf.key] = v
			paths = append(paths, sf.key)
		}
	}
	policyPaths, err := applyPolicyUpdates(cmd, appPolicyKinds, app)
	if err != nil {
		return nil, err
	}
	paths = append(paths, policyPaths...)
	if cmd.Flags().Changed("identity-matching") {
		v, _ := cmd.Flags().GetString("identity-matching")
		api, err := mapIdentityMatching(v)
		if err != nil {
			return nil, err
		}
		app["identityMatching"] = api
		paths = append(paths, "identityMatching")
	}
	if cmd.Flags().Changed("app-user-mapper-file") {
		mapperFile, _ := cmd.Flags().GetString("app-user-mapper-file")
		if mapperFile == "" {
			return nil, fmt.Errorf(`--app-user-mapper-file requires a path (or "-" for stdin)`)
		}
		mapper, err := readAppUserMapperFile(cmd, mapperFile)
		if err != nil {
			return nil, err
		}
		app["appUserMapper"] = mapper
		paths = append(paths, "appUserMapper")
	}

	if len(paths) == 0 {
		return nil, fmt.Errorf("nothing to update: pass --display-name, --description, --grant-policy-id, --revoke-policy-id, --certify-policy-id, --identity-matching, or --app-user-mapper-file")
	}
	return map[string]any{"app": app, "updateMask": strings.Join(paths, ",")}, nil
}

func init() {
	f := appsUpdateCmd.Flags()
	f.String("display-name", "", "New display name (omit to leave unchanged; cannot be empty or whitespace-only)")
	f.String("description", "", "New description; pass an empty value to clear it (omit to leave unchanged)")
	registerPolicyIDFlags(appsUpdateCmd, appPolicyKinds, " (omit to leave unchanged)")
	registerUnbindPolicyFlag(appsUpdateCmd, appPolicyKinds)
	f.String("identity-matching", "", "How app users are matched to C1.ai users: strict, display-name, custom, or a full APP_USER_IDENTITY_MATCHING_* value (omit to leave unchanged)")
	f.String("app-user-mapper-file", "", `JSON object {"mappingCases":[{"userKeyCel":...,"appUserKeyCel":...}]} that replaces the whole app user mapper (file, or "-" for stdin)`)
	appsCmd.AddCommand(appsUpdateCmd)
}
