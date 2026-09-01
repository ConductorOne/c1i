package cmd

import (
	"encoding/json"
	"fmt"

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
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newPoliciesClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		query, _ := cmd.Flags().GetString("query")
		displayName, _ := cmd.Flags().GetString("display-name")
		includeDeleted, _ := cmd.Flags().GetBool("include-deleted")
		policyTypes, err := repeatableStringFlag(cmd, "policy-type")
		if err != nil {
			return err
		}
		excludeIDs, err := repeatableStringFlag(cmd, "exclude-policy-id")
		if err != nil {
			return err
		}
		requestedPageSize := pageSizeFlag(cmd)
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		enc := newEmitter(cmd)
		for !limitReached(enc.Written(), limit) {
			pageSize := requestedPageSize
			if !enc.Filtered() {
				pageSize = effectivePageSize(requestedPageSize, limit, enc.Written())
			}
			body := map[string]any{
				"pageSize": pageSize,
			}
			if pageToken != "" {
				body["pageToken"] = pageToken
			}
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
				mapped := make([]string, len(policyTypes))
				for i, t := range policyTypes {
					mapped[i] = mapPolicyType(t)
				}
				body["policyTypes"] = mapped
			}
			if len(excludeIDs) > 0 {
				body["excludePolicyIds"] = excludeIDs
			}

			data, err := c.Post(cmd.Context(), "/api/v1/search/policies", body)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List          []policyListItem `json:"list"`
				NextPageToken string           `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, p := range resp.List {
				_ = enc.Encode(policyRow(p))
				if limitReached(enc.Written(), limit) {
					return nil
				}
			}

			if resp.NextPageToken == "" || manualPaging {
				break
			}
			pageToken = resp.NextPageToken
		}

		return nil
	},
}

func init() {
	policiesSearchCmd.Flags().String("query", "", "Fuzzy search on display name and description")
	policiesSearchCmd.Flags().String("display-name", "", "Exact-ish (case-insensitive) display name match")
	addRepeatableStringFlag(policiesSearchCmd, "policy-type", "Filter by policy type: grant, revoke, certify, ... (repeatable)")
	policiesSearchCmd.Flags().Bool("include-deleted", false, "Include soft-deleted policies")
	addRepeatableStringFlag(policiesSearchCmd, "exclude-policy-id", "Policy ID to exclude from results (repeatable)")
	// The lower default is this endpoint's own; the rest is context the shared
	// flag wording can't carry without drifting. The floor here is 5, not the
	// 10 the policy proto's comment claims (9 passes through unclamped) -- and
	// 5 is this endpoint's number, not a universal one. Verified live.
	addPaginationFlagsWithMax(policiesSearchCmd, policiesSearchDefaultPageSize, maxPageSize)
	policiesCmd.AddCommand(policiesSearchCmd)
}
