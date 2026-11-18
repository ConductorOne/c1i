package cmd

import "github.com/spf13/cobra"

var grantsCmd = &cobra.Command{
	Use:   "grants",
	Short: "Query access grants (who has access to what), or change them directly",
	Long: `Query grants — the bindings between accounts/users and entitlements — or
change them directly: "grants add", "grants remove", and "grants
update-duration"/"remove-duration" for when a grant is revoked automatically.

Answers "who has access": filter by --entitlement-id (with --app-id) to see who
holds an entitlement, by --user-id to see what a C1 identity has, by
--app-user-id for a specific app account, or by --app-id for all grants in an
app. At least one filter is required.`,
}

func init() {
	rootCmd.AddCommand(grantsCmd)
}
