package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var catalogsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a request catalog / access profile (pretty JSON)",
	Long: `Create a request catalog (an "access profile" in the C1 UI).

Only --display-name is required. Every other flag is omitted from the request
body unless you pass it, so the server's own defaults apply.

The new catalog is returned as pretty JSON under requestCatalogView (--fields
is not applied to mutation output), so read the id from
.requestCatalogView.requestCatalog.id.

--published and --visible-to-everyone both take effect at create time: a
catalog can be created already published. Ordering matters for the visibility
bindings that gate a catalog that is NOT visible to everyone — adding an access
entitlement to an unpublished catalog is refused with a 400, "catalog must be
published to add an access entitlement", so publish first.

Example:
  CAT_ID=$(c1i catalogs create --display-name "Engineering" --published | jq -r .requestCatalogView.requestCatalog.id)
  c1i catalogs get "$CAT_ID"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "display-name"); err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		body := buildCatalogCreateBody(cmd)

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

// buildCatalogCreateBody assembles the Create request body from flags. Pure (no
// network / auth) so the dry-run preview and unit tests exercise the same body
// the live request sends.
func buildCatalogCreateBody(cmd *cobra.Command) map[string]any {
	displayName, _ := cmd.Flags().GetString("display-name")
	body := map[string]any{"displayName": displayName}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
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
	f := catalogsCreateCmd.Flags()
	f.String("display-name", "", "Display name for the new catalog")
	f.String("description", "", "Description for the new catalog")
	f.Bool("published", false, "Create the catalog already published (omit to leave it unset)")
	f.Bool("visible-to-everyone", false, "Let every user see the catalog regardless of its access entitlements; while set, the API refuses to add new ones (\"catalog is visible to everyone, cannot add access entitlements\") (omit to leave it unset)")
	f.Bool("request-bundle", false, "Allow requesting every entitlement in the catalog at once; the API spec notes \"Your tenant must have the bundles feature to use this\" (omit to leave it unset)")
	markRequired(catalogsCreateCmd, "display-name")
	catalogsCmd.AddCommand(catalogsCreateCmd)
}
