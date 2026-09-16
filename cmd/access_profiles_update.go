package cmd

import (
	"fmt"
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

Behavior flags accept their short values (for example, "bypass", "enforce",
"revoke-all") or the full REQUEST_CATALOG_* enum value.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := buildAccessProfileUpdateBody(cmd, args[0])
		if err != nil {
			return &usageError{err}
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		path := client.Path("/api/v1/catalogs/%s", args[0])
		if dryRunActive() {
			return printDryRun(cmd, "POST", path, body)
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Post(cmd.Context(), path, body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeRawObject(cmd, data)
	},
}

// buildAccessProfileUpdateBody builds the Update request wrapper from changed
// flags. The catalog ID is part of the nested object and each mask path matches
// exactly one supplied catalog field.
func buildAccessProfileUpdateBody(cmd *cobra.Command, id string) (map[string]any, error) {
	catalog := map[string]any{"id": id}
	paths := addCatalogChangedFields(cmd, catalog)
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
	f.String("display-name", "", "New display name (omit to leave unchanged)")
	f.String("description", "", "New description; pass an empty value to clear it (omit to leave unchanged)")
	f.Bool("published", false, "Publish or unpublish the access profile (omit to leave unchanged)")
	f.Bool("visible-to-everyone", false, "Set whether every user can see the access profile (omit to leave unchanged)")
	f.Bool("request-bundle", false, "Set whether all profile entitlements can be requested together (omit to leave unchanged)")
	f.String("enrollment-behavior", "", "Enrollment request-policy behavior: bypass, enforce, unspecified, or a full REQUEST_CATALOG_ENROLLMENT_BEHAVIOR_* enum (omit to leave unchanged)")
	f.String("unenrollment-behavior", "", "Unenrollment behavior: leave-access-as-is, revoke-all, revoke-unjustified, unspecified, or a full REQUEST_CATALOG_UNENROLLMENT_BEHAVIOR_* enum (omit to leave unchanged)")
	f.String("unenrollment-entitlement-behavior", "", "Unenrollment entitlement revoke-policy behavior: bypass, enforce, unspecified, or a full REQUEST_CATALOG_UNENROLLMENT_ENTITLEMENT_BEHAVIOR_* enum (omit to leave unchanged)")
	accessProfilesCmd.AddCommand(accessProfilesUpdateCmd)
}
