package cmd

import (
	"net/http"

	"github.com/spf13/cobra"
)

var appsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new app (pretty JSON)",
	Long: `Create a new app (a container you can register MCP servers under).

Only --display-name is required. This creates a plain, unmanaged container
app — the zero state for "make an app, then register MCP servers under it".
The caller is auto-assigned as an owner of the new app, appearing in
"apps owners" after the usual owner-provisioning lag. Change the list with
"apps add-owner"/"apps remove-owner", or "apps set-owners", which replaces
the whole list -- including you.

--grant-policy-id, --revoke-policy-id and --certify-policy-id bind policies
at creation. Omitted, the server assigns its default grant and revoke
policies and no certify policy. As with "apps update", c1i first checks
each id exists and has the flag's type (exit 2 otherwise, nothing sent);
--dry-run skips the check. An empty policy id is a usage error.
--identity-matching takes the same values as on "apps update".

Prints the created app as pretty JSON, unwrapped like "apps get": the id is
.id. --fields is a usage error on a mutation; extract with jq -er, which fails
on a --dry-run preview where jq -r exits 0 with null.

Example:
  APP_ID=$(c1i apps create --display-name "Google Workspace" | jq -er .id)
  c1i mcp servers register --app-id "$APP_ID" --type hosted ...`,
	RunE: func(cmd *cobra.Command, args []string) error {

		body, err := buildAppCreateBody(cmd)
		if err != nil {
			return &usageError{err}
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: "/api/v1/apps", Body: body},
			withPreRead(policyBindingsCheck(cmd, appPolicyKinds)))
	},
}

// buildAppCreateBody assembles the CreateApp request body from flags. Pure (no
// network / auth) so the dry-run preview and unit tests exercise the same body
// the live request sends. Optional fields are omitted when empty rather than
// sent as empty strings/arrays.
func buildAppCreateBody(cmd *cobra.Command) (map[string]any, error) {
	displayName, _ := cmd.Flags().GetString("display-name")
	body := map[string]any{"displayName": displayName}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}
	if err := applyPolicyIDs(cmd, appPolicyKinds, body); err != nil {
		return nil, err
	}
	if cmd.Flags().Changed("identity-matching") {
		api, err := identityMatchingFlag.parse(mustGetString(cmd, "identity-matching"))
		if err != nil {
			return nil, err
		}
		body["identityMatching"] = api
	}
	return body, nil
}

func init() {
	appsCreateCmd.Flags().String("display-name", "", "Display name for the new app ("+displayNameRule+")")
	appsCreateCmd.Flags().String("description", "", "Description for the new app")
	allowEmpty(appsCreateCmd, emptyIsOmitted, "description")
	registerPolicyIDFlags(appsCreateCmd, appPolicyKinds, " (omit for the server's default)")
	appsCreateCmd.Flags().String("identity-matching", "", "How app users are matched to C1.ai users: "+identityMatchingFlag.usage())
	markRequired(appsCreateCmd, "display-name")
	markMutating(appsCreateCmd)
	appsCmd.AddCommand(appsCreateCmd)
}
