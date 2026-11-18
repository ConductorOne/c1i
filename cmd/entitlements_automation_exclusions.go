package cmd

import (
	"net/http"

	"github.com/spf13/cobra"
)

var entitlementsAutomationExclusionsCmd = &cobra.Command{
	Use:   "exclusions",
	Short: "Manage the users an entitlement's automation leaves alone",
	Long: `Manage an entitlement automation's exclusion list: per the API, "excluded
users are not affected by the automation rule". The list can be read and
changed whether or not the entitlement has a live automation; in testing,
list, add and remove all exited 0 against a deleted one.`,
}

func entitlementAutomationExclusionsPath(cmd *cobra.Command, entitlementID string) string {
	appID, _ := cmd.Flags().GetString("app-id")
	return entitlementAutomationPath(appID, entitlementID) + "/exclusions"
}

var entitlementsAutomationExclusionsListCmd = &cobra.Command{
	Use:   "list <entitlement-id>",
	Short: "List the users excluded from an entitlement's automation (NDJSON output)",
	Long: `List the C1 users excluded from an entitlement's automation. Rows: user_id,
display_name, email, status.

An unknown entitlement exits 4 (the server answers 404). Auto-paginates to
completion like every other list command; c1i clamps --page-size to the
server's 100.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := entitlementAutomationExclusionsPath(cmd, args[0])
		return listMapPages(cmd, "GET "+path, "list", []string{"user_id"}, automationExclusionRow, getPage(cmd.Context(), path, nil))
	},
}

// automationExclusionRow keeps the user: the row's own appEntitlementId and
// appUserId did not match the entitlement or an account in testing.
func automationExclusionRow(item map[string]any) map[string]any {
	u, _ := item["user"].(map[string]any)
	str := func(k string) any { s, _ := u[k].(string); return nilIfEmpty(s) }
	return map[string]any{
		"user_id":      str("id"),
		"display_name": str("displayName"),
		"email":        str("email"),
		"status":       str("status"),
	}
}

const exclusionsWriteNote = `Each --user-id is a C1 user id. The server answers an unknown one with 200 and
stores nothing, so c1i looks the ids up first. Adding a user already excluded,
or removing one who is not, exits 0. The server prints {}.`

var entitlementsAutomationExclusionsAddCmd = &cobra.Command{
	Use:   "add <entitlement-id>",
	Short: "Exclude users from an entitlement's automation",
	Long: `Add users to an entitlement automation's exclusion list, one request for all
ids.

` + exclusionsWriteNote,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendExclusionsChange(cmd, http.MethodPost, args[0])
	},
}

var entitlementsAutomationExclusionsRemoveCmd = &cobra.Command{
	Use:   "remove <entitlement-id>",
	Short: "Stop excluding users from an entitlement's automation",
	Long: `Remove users from an entitlement automation's exclusion list, one request for
all ids.

` + exclusionsWriteNote,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendExclusionsChange(cmd, http.MethodDelete, args[0])
	},
}

func sendExclusionsChange(cmd *cobra.Command, method, entitlementID string) error {
	ids, err := repeatableStringFlag(cmd, "user-id")
	if err != nil {
		return err
	}
	ids = dedupe(ids)
	return sendMutation(cmd, mutation{Method: method, Path: entitlementAutomationExclusionsPath(cmd, entitlementID), Body: map[string]any{"userIds": ids}},
		withPreRead(existingUsersCheck("user-id", ids)))
}

func init() {
	for _, c := range []*cobra.Command{
		entitlementsAutomationExclusionsListCmd,
		entitlementsAutomationExclusionsAddCmd,
		entitlementsAutomationExclusionsRemoveCmd,
	} {
		c.Flags().String("app-id", "", "Application ID the entitlement belongs to (the C1 app)")
		markRequired(c, "app-id")
		entitlementsAutomationExclusionsCmd.AddCommand(c)
	}
	addPaginationFlags(entitlementsAutomationExclusionsListCmd)
	for _, c := range []*cobra.Command{entitlementsAutomationExclusionsAddCmd, entitlementsAutomationExclusionsRemoveCmd} {
		addRepeatableStringFlag(c, "user-id", "C1 user ID (repeatable)")
		markRequired(c, "user-id")
		markMutating(c)
		markUserRefChecked(c, "user-id")
	}
	entitlementsAutomationCmd.AddCommand(entitlementsAutomationExclusionsCmd)
}
