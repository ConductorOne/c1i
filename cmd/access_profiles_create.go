package cmd

import (
	"fmt"
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

--type sets the access profile type: catalog (Catalog in the UI), birthright
(displayed as Role; also accepted as "role"), or hybrid (the default when
--type is omitted). Your tenant must have access profile types enabled to send
a non-unspecified type; the server returns a 400 if the feature is off.

Birthright (Role) profiles do not support entitlement request settings: the
server returns 400 "access profile type Birthright does not support entitlement
request settings" if you pass --published, --enrollment-behavior,
--unenrollment-behavior, or --unenrollment-entitlement-behavior with
--type birthright. Those flags are rejected client-side when --type birthright
is set.

--published and --visible-to-everyone both take effect at create time: a
profile can be created already published. Ordering matters for the visibility
bindings on a profile published but not visible to everyone — adding an access
entitlement to an unpublished profile is refused with a 400, "catalog must be
published to add an access entitlement", so publish first. Measured caveat: in
one run both a requestable and a visibility add on an unpublished profile
returned 200 {} and the binding was stored, so treat publishing first as
required for the binding to take effect, not as an enforced guard; verify with
"access-profiles get".

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
	enumUpdate("type", "type", &catalogTypeFlag),
	enumUpdate("enrollment-behavior", "enrollmentBehavior", &catalogEnrollmentFlag),
	enumUpdate("unenrollment-behavior", "unenrollmentBehavior", &catalogUnenrollmentFlag),
	enumUpdate("unenrollment-entitlement-behavior", "unenrollmentEntitlementBehavior", &catalogUnenrollmentEntitlementFlag),
	boolUpdate("published", "published"),
	boolUpdate("visible-to-everyone", "visibleToEveryone"),
	boolUpdate("request-bundle", "requestBundle"),
}}

var catalogTypeFlag = enumFlag{flag: "type", prefix: "REQUEST_CATALOG_TYPE_", unknown: enumPassPrefixed, values: []enumValue{
	{name: "birthright", api: "REQUEST_CATALOG_TYPE_BUNDLE", aliases: []string{"role"}},
	{name: "catalog", api: "REQUEST_CATALOG_TYPE_CATALOG"},
	{name: "hybrid", api: "REQUEST_CATALOG_TYPE_CATALOG_AND_BUNDLE"},
	{name: "unspecified", api: "REQUEST_CATALOG_TYPE_UNSPECIFIED"},
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
	if err := validateBirthrightConstraints(cmd, body); err != nil {
		return nil, err
	}
	return body, nil
}

// validateBirthrightConstraints rejects flags that the server refuses for
// Birthright (type=BUNDLE) profiles, with the server's own error quoted so
// the message is greppable from either direction.
func validateBirthrightConstraints(cmd *cobra.Command, body map[string]any) error {
	if body["type"] != "REQUEST_CATALOG_TYPE_BUNDLE" {
		return nil
	}
	const serverMsg = `"access profile type Birthright does not support entitlement request settings"`
	birthrightBlockedFlags := []string{"published", "enrollment-behavior", "unenrollment-behavior", "unenrollment-entitlement-behavior"}
	for _, flag := range birthrightBlockedFlags {
		if cmd.Flags().Changed(flag) {
			return &usageError{fmt.Errorf("--%s is not supported on --type birthright profiles: the server returns %s", flag, serverMsg)}
		}
	}
	return nil
}

func init() {
	f := accessProfilesCreateCmd.Flags()
	f.String("display-name", "", "Display name for the new access profile ("+displayNameRule+")")
	f.String("description", "", `Description for the new access profile; "" is sent as an empty value`)
	f.String("type", "", "Access profile type: "+catalogTypeFlag.usage()+" (omit for the server default; not supported by all tenants; birthright profiles reject --published and enrollment/unenrollment behavior flags)")
	f.Bool("published", false, "Create the access profile already published (omit to leave it unset; not allowed with --type birthright)")
	f.Bool("visible-to-everyone", false, "Let every user see the access profile regardless of its access entitlements; while set, the API refuses to add new ones (\"catalog is visible to everyone, cannot add access entitlements\") (omit to leave it unset)")
	f.Bool("request-bundle", false, "Allow requesting every entitlement in the profile at once; the API spec notes \"Your tenant must have the bundles feature to use this\" (omit to leave it unset)")
	f.String("enrollment-behavior", "", "Enrollment request-policy behavior: "+catalogEnrollmentFlag.usage()+` (omit to leave it unset; "" is sent and the server rejects it as an invalid enum; not allowed with --type birthright)`)
	f.String("unenrollment-behavior", "", "Unenrollment behavior: "+catalogUnenrollmentFlag.usage()+` (omit to leave it unset; "" is sent and the server rejects it as an invalid enum; not allowed with --type birthright)`)
	f.String("unenrollment-entitlement-behavior", "", "Unenrollment entitlement revoke-policy behavior: "+catalogUnenrollmentEntitlementFlag.usage()+` (omit to leave it unset; "" is sent and the server rejects it as an invalid enum; not allowed with --type birthright)`)
	allowEmpty(accessProfilesCreateCmd, emptyIsSent, "description", "enrollment-behavior", "unenrollment-behavior", "unenrollment-entitlement-behavior")
	markRequired(accessProfilesCreateCmd, "display-name")
	markMutating(accessProfilesCreateCmd)
	accessProfilesCmd.AddCommand(accessProfilesCreateCmd)
}
