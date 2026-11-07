package cmd

import (
	"fmt"
	"net/http"
	"strings"

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
	if _, err := requireNonEmptyIfSet(cmd, "display-name"); err != nil {
		return nil, err
	}
	catalog := map[string]any{"id": id}
	paths, err := addCatalogChangedFields(cmd, catalog)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("nothing to update: pass a writable access-profile field")
	}
	return map[string]any{
		"catalog":    catalog,
		"updateMask": strings.Join(paths, ","),
	}, nil
}

func init() {
	f := accessProfilesUpdateCmd.Flags()
	f.String("display-name", "", "New display name (omit to leave unchanged; "+displayNameRule+")")
	f.String("description", "", "New description; pass an empty value to clear it (omit to leave unchanged)")
	f.Bool("published", false, "Publish or unpublish the access profile (omit to leave unchanged)")
	f.Bool("visible-to-everyone", false, "Set whether every user can see the access profile (omit to leave unchanged)")
	f.Bool("request-bundle", false, "Set whether all profile entitlements can be requested together (omit to leave unchanged)")
	f.String("enrollment-behavior", "", "Enrollment request-policy behavior: "+catalogEnrollmentFlag.usage()+" (omit to leave unchanged)")
	f.String("unenrollment-behavior", "", "Unenrollment behavior: "+catalogUnenrollmentFlag.usage()+" (omit to leave unchanged)")
	f.String("unenrollment-entitlement-behavior", "", "Unenrollment entitlement revoke-policy behavior: "+catalogUnenrollmentEntitlementFlag.usage()+" (omit to leave unchanged)")
	markMutating(accessProfilesUpdateCmd)
	accessProfilesCmd.AddCommand(accessProfilesUpdateCmd)
}
