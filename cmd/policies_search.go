package cmd

import (
	"github.com/spf13/cobra"
)

// policiesSearchDefaultPageSize is this endpoint's own default, lower than
// defaultPageSize: 0 means the server's default of 25, so 25 is what the
// flag would effectively send anyway.
const policiesSearchDefaultPageSize = 25

var policiesSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search policies by display name, description, type, or deletion state (NDJSON output)",
	Long: `Search policies. Unlike "policies list" (pagination only), this filters by
a fuzzy query (display name + description), a case-insensitive display-name
match, one or more policy types, and can include soft-deleted policies via
--include-deleted — the only listing that can find one. ("policies get"
also still returns a deleted policy directly, by id, with deletedAt
populated; it's "policies list" and the default "policies search" that
exclude them.)

Rows are the same shape "policies list" emits, including step_kinds (the kind
of each step in the policy's baseline sequence) and baseline_policy_id — see
"policies list --help" for what they mean and the jq recipe that identifies a
tenant's auto-approval policy portably. --display-name is no substitute:
it only ignores case, and the names differ by more than that between tenants
("Auto-approval" in one, "Auto approval" in the next).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		policyTypes, err := parseRepeatableEnum(cmd, policyTypeFlag)
		if err != nil {
			return err
		}
		excludeIDs, err := repeatableStringFlag(cmd, "exclude-policy-id")
		if err != nil {
			return err
		}
		c, err := listClientVia(cmd, newPoliciesClient)
		if err != nil {
			return err
		}
		query, _ := cmd.Flags().GetString("query")
		displayName, _ := cmd.Flags().GetString("display-name")
		includeDeleted, _ := cmd.Flags().GetBool("include-deleted")
		body := map[string]any{}
		if query != "" {
			body["query"] = query
		}
		if displayName != "" {
			body["displayName"] = displayName
		}
		if includeDeleted {
			body["includeDeleted"] = true
		}
		if len(policyTypes) > 0 {
			body["policyTypes"] = policyTypes
		}
		if len(excludeIDs) > 0 {
			body["excludePolicyIds"] = excludeIDs
		}
		return listPages(cmd, c, pageSpec[policyListItem]{
			Route: "POST /api/v1/search/policies",
			Fetch: postPage(cmd.Context(), "/api/v1/search/policies", body),
			Parse: parseList[policyListItem],
			ID:    func(it policyListItem) string { return it.ID },
			Rows:  rowFn(policyRow),
		})
	},
}

func init() {
	policiesSearchCmd.Flags().String("query", "", "Fuzzy search on display name and description")
	policiesSearchCmd.Flags().String("display-name", "", "Exact-ish (case-insensitive) display name match")
	addRepeatableStringFlag(policiesSearchCmd, "policy-type", "Filter by policy type: "+policyTypeFlag.usage()+" (repeatable)")
	policiesSearchCmd.Flags().Bool("include-deleted", false, "Include soft-deleted policies")
	addRepeatableStringFlag(policiesSearchCmd, "exclude-policy-id", "Policy ID to exclude from results (repeatable)")
	// The lower default is this endpoint's own; the rest is context the shared
	// flag wording can't carry without drifting. The floor here is 5, not the
	// 10 the policy proto's comment claims (9 passes through unclamped) -- and
	// 5 is this endpoint's number, not a universal one. Verified live.
	addPaginationFlagsWithMax(policiesSearchCmd, policiesSearchDefaultPageSize, maxPageSize)
	policiesCmd.AddCommand(policiesSearchCmd)
}
