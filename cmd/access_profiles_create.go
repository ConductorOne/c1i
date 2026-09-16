package cmd

import (
	"fmt"
	"strings"

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

// catalogStringFlags maps the string-valued RequestCatalog fields exposed as
// flags to their protojson keys and normalizes the behavior enum shortcuts.
// The same fields are valid on both create and update.
var catalogStringFlags = []struct {
	flag, key string
	mapValue  func(string) string
}{
	{"display-name", "displayName", func(v string) string { return v }},
	{"description", "description", func(v string) string { return v }},
	{"enrollment-behavior", "enrollmentBehavior", mapCatalogEnrollmentBehavior},
	{"unenrollment-behavior", "unenrollmentBehavior", mapCatalogUnenrollmentBehavior},
	{"unenrollment-entitlement-behavior", "unenrollmentEntitlementBehavior", mapCatalogUnenrollmentEntitlementBehavior},
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
func addCatalogChangedFields(cmd *cobra.Command, catalog map[string]any) []string {
	var paths []string
	for _, sf := range catalogStringFlags {
		if cmd.Flags().Changed(sf.flag) {
			v, _ := cmd.Flags().GetString(sf.flag)
			catalog[sf.key] = sf.mapValue(v)
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
	return paths
}

func mapCatalogEnrollmentBehavior(v string) string {
	switch strings.ToLower(v) {
	case "unspecified":
		return "REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_UNSPECIFIED"
	case "bypass", "bypass-entitlement-request-policy", "bypass_entitlement_request_policy":
		return "REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_BYPASS_ENTITLEMENT_REQUEST_POLICY"
	case "enforce", "enforce-entitlement-request-policy", "enforce_entitlement_request_policy":
		return "REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_ENFORCE_ENTITLEMENT_REQUEST_POLICY"
	default:
		return v
	}
}

func mapCatalogUnenrollmentBehavior(v string) string {
	switch strings.ToLower(v) {
	case "unspecified":
		return "REQUEST_CATALOG_UNENROLLMENT_BEHAVIOR_UNSPECIFIED"
	case "leave-access-as-is", "leave_access_as_is":
		return "REQUEST_CATALOG_UNENROLLMENT_BEHAVIOR_LEAVE_ACCESS_AS_IS"
	case "revoke-all", "revoke_all":
		return "REQUEST_CATALOG_UNENROLLMENT_BEHAVIOR_REVOKE_ALL"
	case "revoke-unjustified", "revoke_unjustified":
		return "REQUEST_CATALOG_UNENROLLMENT_BEHAVIOR_REVOKE_UNJUSTIFIED"
	default:
		return v
	}
}

func mapCatalogUnenrollmentEntitlementBehavior(v string) string {
	switch strings.ToLower(v) {
	case "unspecified":
		return "REQUEST_CATALOG_UNENROLLMENT_ENTITLEMENT_BEHAVIOR_UNSPECIFIED"
	case "bypass":
		return "REQUEST_CATALOG_UNENROLLMENT_ENTITLEMENT_BEHAVIOR_BYPASS"
	case "enforce":
		return "REQUEST_CATALOG_UNENROLLMENT_ENTITLEMENT_BEHAVIOR_ENFORCE"
	default:
		return v
	}
}

// buildAccessProfileCreateBody assembles the Create request body from flags. Pure (no
// network / auth) so the dry-run preview and unit tests exercise the same body
// the live request sends.
func buildAccessProfileCreateBody(cmd *cobra.Command) map[string]any {
	displayName, _ := cmd.Flags().GetString("display-name")
	body := map[string]any{"displayName": displayName}
	addCatalogChangedFields(cmd, body)
	return body
}

func init() {
	f := accessProfilesCreateCmd.Flags()
	f.String("display-name", "", "Display name for the new access profile")
	f.String("description", "", "Description for the new access profile")
	f.Bool("published", false, "Create the access profile already published (omit to leave it unset)")
	f.Bool("visible-to-everyone", false, "Let every user see the access profile regardless of its access entitlements; while set, the API refuses to add new ones (\"catalog is visible to everyone, cannot add access entitlements\") (omit to leave it unset)")
	f.Bool("request-bundle", false, "Allow requesting every entitlement in the profile at once; the API spec notes \"Your tenant must have the bundles feature to use this\" (omit to leave it unset)")
	f.String("enrollment-behavior", "", "Enrollment request-policy behavior: bypass, enforce, unspecified, or a full REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_* enum (omit to leave it unset)")
	f.String("unenrollment-behavior", "", "Unenrollment behavior: leave-access-as-is, revoke-all, revoke-unjustified, unspecified, or a full REQUEST_CATALOG_UNENROLLMENT_BEHAVIOR_* enum (omit to leave it unset)")
	f.String("unenrollment-entitlement-behavior", "", "Unenrollment entitlement revoke-policy behavior: bypass, enforce, unspecified, or a full REQUEST_CATALOG_UNENROLLMENT_ENTITLEMENT_BEHAVIOR_* enum (omit to leave it unset)")
	markRequired(accessProfilesCreateCmd, "display-name")
	accessProfilesCmd.AddCommand(accessProfilesCreateCmd)
}
