package cmd

import (
	"fmt"

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

The created app is returned as pretty JSON under an "app" key (--fields is not
applied to mutation output, so parse the id from the full object).

Example:
  APP_ID=$(c1i apps create --display-name "Google Workspace" | jq -r .app.id)
  c1i mcp servers register --app-id "$APP_ID" --type hosted ...`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "display-name"); err != nil {
			return err
		}

		body, err := buildAppCreateBody(cmd)
		if err != nil {
			return &usageError{err}
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		if dryRunActive() {
			return printDryRun(cmd, "POST", "/api/v1/apps", body)
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		if err := validatePolicyBindings(cmd.Context(), cmd, c, appPolicyKinds); err != nil {
			return err
		}
		data, err := c.Post(cmd.Context(), "/api/v1/apps", body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeRawObject(cmd, data)
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
		v, _ := cmd.Flags().GetString("identity-matching")
		api, err := mapIdentityMatching(v)
		if err != nil {
			return nil, err
		}
		body["identityMatching"] = api
	}
	return body, nil
}

func init() {
	appsCreateCmd.Flags().String("display-name", "", "Display name for the new app")
	appsCreateCmd.Flags().String("description", "", "Description for the new app")
	registerPolicyIDFlags(appsCreateCmd, appPolicyKinds, " (omit for the server's default)")
	appsCreateCmd.Flags().String("identity-matching", "", "How app users are matched to C1.ai users: strict, display-name, custom, or a full APP_USER_IDENTITY_MATCHING_* value")
	markRequired(appsCreateCmd, "display-name")
	appsCmd.AddCommand(appsCreateCmd)
}
