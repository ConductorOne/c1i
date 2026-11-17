package cmd

import (
	"net/http"

	"github.com/spf13/cobra"
)

var accessProfilesCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an access profile (pretty JSON)",
	Long: `Create an access profile.

Only --display-name is required. Every other flag is omitted from the request
body unless you pass it, so the server's own defaults apply.

Prints the created profile as pretty JSON, unwrapped like "access-profiles
get": the id is .id and the member count .memberCount. --fields is a usage
error on a mutation; extract with jq -er, which fails on a --dry-run preview
where jq -r exits 0 with null.

--published and --visible-to-everyone both take effect at create time: a
profile can be created already published. Ordering matters for the visibility
bindings on a profile published but not visible to everyone — adding an access
entitlement to an unpublished profile is refused with a 400, "catalog must be
published to add an access entitlement", so publish first.

Example:
  CAT_ID=$(c1i access-profiles create --display-name "Engineering" --published | jq -er .id)
  c1i access-profiles get "$CAT_ID"`,
	RunE: func(cmd *cobra.Command, args []string) error {

		body, err := buildAccessProfileCreateBody(cmd)
		if err != nil {
			return err
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: "/api/v1/catalogs", Body: body})
	},
}

// catalogFields are the writable scalar RequestCatalog fields, valid on both
// create and update, in mask order. A set bool keeps an explicit false.
var catalogFields = &updateSpec{fields: []updateField{
	strUpdate("display-name", "displayName"),
	strUpdate("description", "description"),
	enumUpdate("enrollment-behavior", "enrollmentBehavior", &catalogEnrollmentFlag),
	enumUpdate("unenrollment-behavior", "unenrollmentBehavior", &catalogUnenrollmentFlag),
	enumUpdate("unenrollment-entitlement-behavior", "unenrollmentEntitlementBehavior", &catalogUnenrollmentEntitlementFlag),
	boolUpdate("published", "published"),
	boolUpdate("visible-to-everyone", "visibleToEveryone"),
	boolUpdate("request-bundle", "requestBundle"),
}}

var catalogEnrollmentFlag = enumFlag{flag: "enrollment-behavior", prefix: "REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_", unknown: enumPassPrefixed, values: []enumValue{
	{name: "bypass", api: "REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_BYPASS_ENTITLEMENT_REQUEST_POLICY", aliases: []string{"REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_BYPASS"}},
	{name: "enforce", api: "REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_ENFORCE_ENTITLEMENT_REQUEST_POLICY", aliases: []string{"REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_ENFORCE"}},
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
	if _, err := catalogFields.apply(cmd, body); err != nil {
		return nil, err
	}
	return body, nil
}

func init() {
	f := accessProfilesCreateCmd.Flags()
	f.String("display-name", "", "Display name for the new access profile ("+displayNameRule+")")
	f.String("description", "", `Description for the new access profile; "" is sent as an empty value`)
	f.Bool("published", false, "Create the access profile already published (omit to leave it unset)")
	f.Bool("visible-to-everyone", false, "Let every user see the access profile regardless of its access entitlements; while set, the API refuses to add new ones (\"catalog is visible to everyone, cannot add access entitlements\") (omit to leave it unset)")
	f.Bool("request-bundle", false, "Allow requesting every entitlement in the profile at once; the API spec notes \"Your tenant must have the bundles feature to use this\" (omit to leave it unset)")
	f.String("enrollment-behavior", "", "Enrollment request-policy behavior: "+catalogEnrollmentFlag.usage()+` ("" is sent as an empty value; omit to leave it unset)`)
	f.String("unenrollment-behavior", "", "Unenrollment behavior: "+catalogUnenrollmentFlag.usage()+` ("" is sent as an empty value; omit to leave it unset)`)
	f.String("unenrollment-entitlement-behavior", "", "Unenrollment entitlement revoke-policy behavior: "+catalogUnenrollmentEntitlementFlag.usage()+` ("" is sent as an empty value; omit to leave it unset)`)
	allowEmpty(accessProfilesCreateCmd, emptyIsSent, "description", "enrollment-behavior", "unenrollment-behavior", "unenrollment-entitlement-behavior")
	markRequired(accessProfilesCreateCmd, "display-name")
	markMutating(accessProfilesCreateCmd)
	accessProfilesCmd.AddCommand(accessProfilesCreateCmd)
}
