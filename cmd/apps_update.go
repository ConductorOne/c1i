package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var appsUpdateCmd = &cobra.Command{
	Use:   "update <app-id>",
	Short: "Update an app's name, description, policies, or identity matching (pretty JSON)",
	Long: `Update writable app fields. Prints the updated app, unwrapped like
"apps get" (the id is .id).

The API requires {"app": {"id": "...", ...}, "updateMask": "..."}. The update
mask is derived exactly from the flags you pass: an omitted flag is left
alone, while an explicit empty value is a change.

A blank --display-name (empty, whitespace or invisible-only) exits 2,
nothing sent. Sent as is, "" made c1i exit 0 while the server kept the old
name, and the server stores "  " verbatim.

--grant-policy-id, --revoke-policy-id and --certify-policy-id bind a policy
to the app. Before sending, c1i fetches each policy and refuses (nothing sent)
an id that doesn't exist (exit 4), is deleted, or isn't of the flag's type
(POLICY_TYPE_GRANT, _REVOKE or _CERTIFY; exit 2). --dry-run does the check too.

To unbind, pass --unbind-policy grant|revoke|certify (repeatable). An empty
policy id is a usage error rather than an unbind, so an unset shell variable
can't silently remove a policy; setting and unbinding the same kind is too.

--identity-matching accepts strict, display-name, custom, or the full
APP_USER_IDENTITY_MATCHING_* name of one of them; anything else is a usage
error (exit 2) before any request is sent.

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
		body, err := buildAppUpdateBody(cmd, args[0])
		if err != nil {
			return &usageError{err}
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: client.Path("/api/v1/apps/%s", args[0]), Body: body},
			withPolicyBindingsCheck(cmd, appPolicyKinds))
	},
}

// UNSPECIFIED is deliberately absent: an update to it exits 0 but leaves the
// current value in place. That is also why unknowns are rejected.
var identityMatchingFlag = enumFlag{flag: "identity-matching", prefix: "APP_USER_IDENTITY_MATCHING_", unknown: enumReject, values: []enumValue{
	{name: "strict", api: "APP_USER_IDENTITY_MATCHING_STRICT"},
	{name: "display-name", api: "APP_USER_IDENTITY_MATCHING_DISPLAY_NAME"},
	{name: "custom", api: "APP_USER_IDENTITY_MATCHING_CUSTOM"},
}}

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
	objs, err := objectElements("--app-user-mapper-file mappingCases", cases)
	if err != nil {
		return nil, err
	}
	for i, obj := range objs {
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

// appUpdateSpec lists the App's updatable fields, in mask order.
var appUpdateSpec = &updateSpec{
	fields: []updateField{
		strUpdate("display-name", "displayName"),
		strUpdate("description", "description"),
		policyUpdate(appPolicyKinds),
		enumUpdate("identity-matching", "identityMatching", &identityMatchingFlag),
		hookUpdate([]string{"app-user-mapper-file"}, func(cmd *cobra.Command, obj map[string]any, _ any) ([]string, error) {
			if !cmd.Flags().Changed("app-user-mapper-file") {
				return nil, nil
			}
			mapper, err := readAppUserMapperFile(cmd, mustGetString(cmd, "app-user-mapper-file"))
			if err != nil {
				return nil, err
			}
			obj["appUserMapper"] = mapper
			return []string{"appUserMapper"}, nil
		}),
	},
}

// buildAppUpdateBody builds the Update request wrapper from changed flags.
func buildAppUpdateBody(cmd *cobra.Command, id string) (map[string]any, error) {
	app := map[string]any{"id": id}
	mask, err := appUpdateSpec.build(cmd, app, nil)
	if err != nil {
		return nil, err
	}
	return wrapUpdate("app", app, mask), nil
}

func init() {
	f := appsUpdateCmd.Flags()
	f.String("display-name", "", "New display name (omit to leave unchanged; "+displayNameRule+")")
	f.String("description", "", `New description; "" clears it (omit to leave unchanged)`)
	allowEmpty(appsUpdateCmd, emptyIsSent, "description")
	registerPolicyIDFlags(appsUpdateCmd, appPolicyKinds, " (omit to leave unchanged)")
	registerUnbindPolicyFlag(appsUpdateCmd, appPolicyKinds)
	registerUpdateSpec(appsUpdateCmd, appUpdateSpec)
	markMutating(appsUpdateCmd)
	f.String("identity-matching", "", "How app users are matched to C1.ai users: "+identityMatchingFlag.usage()+" (omit to leave unchanged)")
	f.String("app-user-mapper-file", "", `JSON object {"mappingCases":[{"userKeyCel":...,"appUserKeyCel":...}]} that replaces the whole app user mapper (file, or "-" for stdin)`)
	appsCmd.AddCommand(appsUpdateCmd)
}
