package cmd

import (
	"github.com/spf13/cobra"
)

// appsSearchDefaultPageSize is this endpoint's documented default.
const appsSearchDefaultPageSize = 25

var appsSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search applications by name, description, id, policy, or directory (NDJSON output)",
	Long: `Search applications. Unlike "apps list" (pagination only), this filters
server-side: --query is a fuzzy match on display name and description,
--display-name a case-insensitive display-name match, --app-id and
--exclude-app-id restrict or remove app ids, --policy-id keeps apps that use
any of the given policies, and --only-directories sets the server's
onlyDirectories filter. Repeatable flags may be given more than once.

Rows are the same shape "apps list" emits.

` + userCountNote + `

Each --policy-id is looked up first: an unknown id exits 4 and a deleted one
exits 2, before the search is sent. An --app-id the tenant doesn't have simply
matches nothing (observed: an empty result, exit 0). An id that is not 27
letters and digits is rejected by the server (exit 2).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		appIDs, err := repeatableStringFlag(cmd, "app-id")
		if err != nil {
			return err
		}
		excludeIDs, err := repeatableStringFlag(cmd, "exclude-app-id")
		if err != nil {
			return err
		}
		policyIDs, err := repeatableStringFlag(cmd, "policy-id")
		if err != nil {
			return err
		}
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		for _, id := range dedupe(policyIDs) {
			if _, err := fetchBoundPolicyType(cmd.Context(), c, "policy-id", id); err != nil {
				return err
			}
		}
		query, _ := cmd.Flags().GetString("query")
		displayName, _ := cmd.Flags().GetString("display-name")
		onlyDirectories, _ := cmd.Flags().GetBool("only-directories")
		body := map[string]any{}
		if query != "" {
			body["query"] = query
		}
		if displayName != "" {
			body["displayName"] = displayName
		}
		if len(appIDs) > 0 {
			body["appIds"] = appIDs
		}
		if len(excludeIDs) > 0 {
			body["excludeAppIds"] = excludeIDs
		}
		if len(policyIDs) > 0 {
			refs := make([]map[string]string, 0, len(policyIDs))
			for _, id := range policyIDs {
				refs = append(refs, map[string]string{"id": id})
			}
			body["policyRefs"] = refs
		}
		if onlyDirectories {
			body["onlyDirectories"] = true
		}
		return listPages(cmd, c, pageSpec[appListItem]{
			Route: "POST /api/v1/search/apps",
			Fetch: postPage(cmd.Context(), "/api/v1/search/apps", body),
			Parse: parseList[appListItem],
			ID:    func(a appListItem) string { return a.ID },
			Rows:  rowFn(appRow),
		})
	},
}

func init() {
	appsSearchCmd.Flags().String("query", "", "Fuzzy search on display name and description")
	appsSearchCmd.Flags().String("display-name", "", "Case-insensitive display name match")
	addRepeatableStringFlag(appsSearchCmd, "app-id", "Restrict to this app ID (repeatable)")
	addRepeatableStringFlag(appsSearchCmd, "exclude-app-id", "App ID to exclude from results (repeatable)")
	addRepeatableStringFlag(appsSearchCmd, "policy-id", "Only apps that use this policy ID; any of several (repeatable)")
	appsSearchCmd.Flags().Bool("only-directories", false, "Set the server's onlyDirectories filter")
	addPaginationFlagsWithMax(appsSearchCmd, appsSearchDefaultPageSize, maxPageSize)
	appsCmd.AddCommand(appsSearchCmd)
}
