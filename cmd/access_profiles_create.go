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
		if _, err := requireNonEmptyIfSet(cmd, "display-name"); err != nil {
			return err
		}
		if err := requireNonEmpty(cmd, "display-name"); err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		body, err := buildAccessProfileCreateBody(cmd)
		if err != nil {
			return err
		}

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

// catalogStringFlags maps the string-valued RequestCatalog fields exposed as
// flags to their protojson keys and normalizes the behavior enum shortcuts.
// The same fields are valid on both create and update.
var catalogStringFlags = []struct {
	flag, key string
	enum      *enumFlag
}{
	{"display-name", "displayName", nil},
	{"description", "description", nil},
	{"enrollment-behavior", "enrollmentBehavior", &catalogEnrollmentFlag},
	{"unenrollment-behavior", "unenrollmentBehavior", &catalogUnenrollmentFlag},
	{"unenrollment-entitlement-behavior", "unenrollmentEntitlementBehavior", &catalogUnenrollmentEntitlementFlag},
}

// catalogBoolFlags maps the boolean RequestCatalog fields exposed as flags to
// their protojson keys. Changed, rather than true, preserves an explicit false.
var catalogBoolFlags = []struct{ flag, key string }{
	{"published", "published"},
	{"visible-to-everyone", "visibleToEveryone"},
	{"request-bundle", "requestBundle"},
}

// addCatalogChangedFields applies every explicitly supplied writable scalar
// field to catalog and returns the corresponding FieldMask paths in flag order.
func addCatalogChangedFields(cmd *cobra.Command, catalog map[string]any) ([]string, error) {
	var paths []string
	for _, sf := range catalogStringFlags {
		if cmd.Flags().Changed(sf.flag) {
			v := mustGetString(cmd, sf.flag)
			if sf.enum != nil {
				var err error
				if v, err = sf.enum.parse(v); err != nil {
					return nil, err
				}
			}
			catalog[sf.key] = v
			paths = append(paths, sf.key)
		}
	}
	for _, bf := range catalogBoolFlags {
		if cmd.Flags().Changed(bf.flag) {
			v, _ := cmd.Flags().GetBool(bf.flag)
			catalog[bf.key] = v
			paths = append(paths, bf.key)
		}
	}
	return paths, nil
}

var catalogEnrollmentFlag = enumFlag{flag: "enrollment-behavior", prefix: "REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_", unknown: enumPassPrefixed, values: []enumValue{
	{name: "bypass", api: "REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_BYPASS_ENTITLEMENT_REQUEST_POLICY"},
	{name: "enforce", api: "REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_ENFORCE_ENTITLEMENT_REQUEST_POLICY"},
	{name: "unspecified", api: "REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_UNSPECIFIED"},
}}

var catalogUnenrollmentFlag = enumFlag{flag: "unenrollment-behavior", prefix: "REQUEST_CATALOG_UNENROLLMENT_BEHAVIOR_", unknown: enumPassPrefixed, values: []enumValue{
	{name: "leave-access-as-is", api: "REQUEST_CATALOG_UNENROLLMENT_BEHAVIOR_LEAVE_ACCESS_AS_IS"},
	{name: "revoke-all", api: "REQUEST_CATALOG_UNENROLLMENT_BEHAVIOR_REVOKE_ALL"},
	{name: "revoke-unjustified", api: "REQUEST_CATALOG_UNENROLLMENT_BEHAVIOR_REVOKE_UNJUSTIFIED"},
	{name: "unspecified", api: "REQUEST_CATALOG_UNENROLLMENT_BEHAVIOR_UNSPECIFIED"},
}}

var catalogUnenrollmentEntitlementFlag = enumFlag{flag: "unenrollment-entitlement-behavior", prefix: "REQUEST_CATALOG_UNENROLLMENT_ENTITLEMENT_BEHAVIOR_", unknown: enumPassPrefixed, values: []enumValue{
	{name: "bypass", api: "REQUEST_CATALOG_UNENROLLMENT_ENTITLEMENT_BEHAVIOR_BYPASS"},
	{name: "enforce", api: "REQUEST_CATALOG_UNENROLLMENT_ENTITLEMENT_BEHAVIOR_ENFORCE"},
	{name: "unspecified", api: "REQUEST_CATALOG_UNENROLLMENT_ENTITLEMENT_BEHAVIOR_UNSPECIFIED"},
}}

// buildAccessProfileCreateBody assembles the Create request body from flags. Pure (no
// network / auth) so the dry-run preview and unit tests exercise the same body
// the live request sends.
func buildAccessProfileCreateBody(cmd *cobra.Command) (map[string]any, error) {
	displayName, _ := cmd.Flags().GetString("display-name")
	body := map[string]any{"displayName": displayName}
	if _, err := addCatalogChangedFields(cmd, body); err != nil {
		return nil, err
	}
	return body, nil
}

func init() {
	f := accessProfilesCreateCmd.Flags()
	f.String("display-name", "", "Display name for the new access profile ("+displayNameRule+")")
	f.String("description", "", "Description for the new access profile")
	f.Bool("published", false, "Create the access profile already published (omit to leave it unset)")
	f.Bool("visible-to-everyone", false, "Let every user see the access profile regardless of its access entitlements; while set, the API refuses to add new ones (\"catalog is visible to everyone, cannot add access entitlements\") (omit to leave it unset)")
	f.Bool("request-bundle", false, "Allow requesting every entitlement in the profile at once; the API spec notes \"Your tenant must have the bundles feature to use this\" (omit to leave it unset)")
	f.String("enrollment-behavior", "", "Enrollment request-policy behavior: "+catalogEnrollmentFlag.usage()+" (omit to leave it unset)")
	f.String("unenrollment-behavior", "", "Unenrollment behavior: "+catalogUnenrollmentFlag.usage()+" (omit to leave it unset)")
	f.String("unenrollment-entitlement-behavior", "", "Unenrollment entitlement revoke-policy behavior: "+catalogUnenrollmentEntitlementFlag.usage()+" (omit to leave it unset)")
	markRequired(accessProfilesCreateCmd, "display-name")
	accessProfilesCmd.AddCommand(accessProfilesCreateCmd)
}
