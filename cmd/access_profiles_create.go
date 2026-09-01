package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var accessProfilesCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an access profile (pretty JSON)",
	Long: `Create an access profile.

Only --display-name is required. Every other flag is omitted from the request
body unless you pass it, so the server's own defaults apply.

The new profile is returned as pretty JSON under requestCatalogView (--fields
is not applied to mutation output), so read the id from
.requestCatalogView.requestCatalog.id.

--published and --visible-to-everyone both take effect at create time: a
profile can be created already published. Ordering matters for the visibility
bindings on a profile published but not visible to everyone — adding an access
entitlement to an unpublished profile is refused with a 400, "catalog must be
published to add an access entitlement", so publish first.

Example:
  CAT_ID=$(c1i access-profiles create --display-name "Engineering" --published | jq -r .requestCatalogView.requestCatalog.id)
  c1i access-profiles get "$CAT_ID"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "display-name"); err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		body := buildAccessProfileCreateBody(cmd)

		if dryRunActive() {
			return printDryRun(cmd, "POST", "/api/v1/catalogs", body)
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Post(cmd.Context(), "/api/v1/catalogs", body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeRawObject(cmd, data)
	},
}

// catalogCreateBoolFlags maps each optional boolean flag to its request-body
// key. Only a flag the caller actually passed is sent, so `--published=false`
// is distinguishable from not asking at all.
var catalogCreateBoolFlags = []struct{ flag, key string }{
	{"published", "published"},
	{"visible-to-everyone", "visibleToEveryone"},
	{"request-bundle", "requestBundle"},
}

// buildAccessProfileCreateBody assembles the Create request body from flags. Pure (no
// network / auth) so the dry-run preview and unit tests exercise the same body
// the live request sends.
func buildAccessProfileCreateBody(cmd *cobra.Command) map[string]any {
	displayName, _ := cmd.Flags().GetString("display-name")
	body := map[string]any{"displayName": displayName}
	if cmd.Flags().Changed("description") {
		v, _ := cmd.Flags().GetString("description")
		body["description"] = v
	}
	for _, bf := range catalogCreateBoolFlags {
		if cmd.Flags().Changed(bf.flag) {
			v, _ := cmd.Flags().GetBool(bf.flag)
			body[bf.key] = v
		}
	}
	return body
}

func init() {
	f := accessProfilesCreateCmd.Flags()
	f.String("display-name", "", "Display name for the new access profile")
	f.String("description", "", "Description for the new access profile")
	f.Bool("published", false, "Create the access profile already published (omit to leave it unset)")
	f.Bool("visible-to-everyone", false, "Let every user see the access profile regardless of its access entitlements; while set, the API refuses to add new ones (\"catalog is visible to everyone, cannot add access entitlements\") (omit to leave it unset)")
	f.Bool("request-bundle", false, "Allow requesting every entitlement in the profile at once; the API spec notes \"Your tenant must have the bundles feature to use this\" (omit to leave it unset)")
	markRequired(accessProfilesCreateCmd, "display-name")
	accessProfilesCmd.AddCommand(accessProfilesCreateCmd)
}
