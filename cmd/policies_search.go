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
a fuzzy query (display name + description), an exact display-name match,
one or more policy types, and can include soft-deleted policies via
--include-deleted — the only listing that can find one. ("policies get"
also still returns a deleted policy directly, by id, with deletedAt
populated; it's "policies list" and the default "policies search" that
exclude them.)`,
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
		policyTypes, _ := cmd.Flags().GetStringSlice("policy-type")
		excludeIDs, _ := cmd.Flags().GetStringSlice("exclude-policy-id")
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
	policiesSearchCmd.Flags().StringSlice("policy-type", nil, "Filter by policy type: grant, revoke, certify, ... (repeatable)")
	policiesSearchCmd.Flags().Bool("include-deleted", false, "Include soft-deleted policies")
	policiesSearchCmd.Flags().StringSlice("exclude-policy-id", nil, "Policy ID to exclude from results (repeatable)")
	// Endpoint-specific, and deliberately not in the flag wording (that text
	// is shared so it cannot drift): the server floors the returned count at
	// 5 -- --page-size 3 still yields 5 rows -- and treats 0 as its own
	// default of 25. Floor is 5, not the 10 the policy proto's comment
	// claims: 9 passes through unclamped. Verified live.
	addPaginationFlagsWithMax(policiesSearchCmd, policiesSearchDefaultPageSize, maxPageSize)
	policiesCmd.AddCommand(policiesSearchCmd)
}
