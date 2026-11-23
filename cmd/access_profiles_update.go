package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var accessProfilesUpdateCmd = &cobra.Command{
	Use:   "update <access-profile-id>",
	Short: "Update an access profile (pretty JSON)",
	Long: `Update writable access-profile fields.

The API requires {"catalog": {"id": "...", ...}, "updateMask": "..."}.
The update mask is derived exactly from the flags you pass: an explicit false
or empty --description is a change, while an omitted flag is left alone.
A blank --display-name (empty, whitespace or invisible-only) exits 2,
nothing sent. Sent as is, the server stores "" as an empty name and keeps
"  " verbatim.

Behavior flags accept their short values (for example, "bypass", "enforce",
"revoke-all") or the full REQUEST_CATALOG_* enum value.

--type changes the access profile type (catalog, birthright/role, or hybrid).
Changing type requires access profile types to be enabled for your tenant.
Changing to birthright while the profile has active requests returns 400
"turn off requests before changing the profile type to Birthright"; disable
requests on the profile first. Birthright profiles reject --published and
enrollment/unenrollment behavior flags (exit 2 client-side).

Prints the updated profile as pretty JSON, unwrapped like "access-profiles
get" (the id is .id). --fields is a usage error on a mutation.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := buildAccessProfileUpdateBody(cmd, args[0])
		if err != nil {
			return &usageError{err}
		}

		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: client.Path("/api/v1/catalogs/%s", args[0]), Body: body})
	},
}

// buildAccessProfileUpdateBody builds the Update request wrapper from changed
// flags. The catalog ID is part of the nested object and each mask path matches
// exactly one supplied catalog field.
func buildAccessProfileUpdateBody(cmd *cobra.Command, id string) (map[string]any, error) {
	catalog := map[string]any{"id": id}
	mask, err := catalogFields.buildUpdateMask(cmd, catalog)
	if err != nil {
		return nil, err
	}
	if err := validateBirthrightConstraints(cmd, catalog); err != nil {
		return nil, err
	}
	return wrapUpdate("catalog", catalog, mask), nil
}

func init() {
	f := accessProfilesUpdateCmd.Flags()
	f.String("display-name", "", "New display name (omit to leave unchanged; "+displayNameRule+")")
	f.String("description", "", `New description; "" clears it (omit to leave unchanged)`)
	allowEmpty(accessProfilesUpdateCmd, emptyIsSent, "description")
	f.String("type", "", "New access profile type: "+catalogTypeFlag.usage()+" (omit to leave unchanged; not supported by all tenants; birthright profiles reject --published and enrollment/unenrollment behavior flags)")
	f.Bool("published", false, "Publish or unpublish the access profile (omit to leave unchanged; not allowed with --type birthright)")
	f.Bool("visible-to-everyone", false, "Set whether every user can see the access profile (omit to leave unchanged)")
	f.Bool("request-bundle", false, "Set whether all profile entitlements can be requested together (omit to leave unchanged)")
	f.String("enrollment-behavior", "", "Enrollment request-policy behavior: "+catalogEnrollmentFlag.usage()+` (omit to leave unchanged; "" is sent and the server rejects it as an invalid enum; not allowed with --type birthright)`)
	f.String("unenrollment-behavior", "", "Unenrollment behavior: "+catalogUnenrollmentFlag.usage()+` (omit to leave unchanged; "" is sent and the server rejects it as an invalid enum; not allowed with --type birthright)`)
	f.String("unenrollment-entitlement-behavior", "", "Unenrollment entitlement revoke-policy behavior: "+catalogUnenrollmentEntitlementFlag.usage()+` (omit to leave unchanged; "" is sent and the server rejects it as an invalid enum; not allowed with --type birthright)`)
	allowEmpty(accessProfilesUpdateCmd, emptyIsSent, "enrollment-behavior", "unenrollment-behavior", "unenrollment-entitlement-behavior")
	registerUpdateSpec(accessProfilesUpdateCmd, catalogFields)
	accessProfilesUpdateCmd.Long += `

--enrollment-behavior unspecified is accepted by the server and resets the
setting to its default (measured: ENFORCE becomes BYPASS), so it can change the
current value; --unenrollment-behavior unspecified resets to
LEAVE_ACCESS_AS_IS. The reset value for --unenrollment-entitlement-behavior was
not measured.`
	markMutating(accessProfilesUpdateCmd)
	accessProfilesCmd.AddCommand(accessProfilesUpdateCmd)
}
