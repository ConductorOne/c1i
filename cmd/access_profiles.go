package cmd

import "github.com/spf13/cobra"

var accessProfilesCmd = &cobra.Command{
	Use:   "access-profiles",
	Short: "Manage access profiles (the API calls them request catalogs)",
	Long: `Access profiles control which entitlements are requestable and who can
request them. Admins create them to grant birthright access or to make access
requestable by a chosen audience.

The API calls this object a request catalog and routes it under
/api/v1/catalogs, so its JSON keys and the ids you pass here say "catalog".
The two names refer to the same object; the spec tags its schema
"x-speakeasy-entity: Access_Profile".

Not to be confused with an app catalog, which is the per-user list of what one
user can request, derived from the access profiles they belong to.`,
}

func init() {
	rootCmd.AddCommand(accessProfilesCmd)
}
