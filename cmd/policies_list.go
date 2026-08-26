package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

var policiesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all policies (NDJSON output)",
	Long: `List every policy in the tenant, auto-paginating through the full result.

Unlike most list commands in this CLI, GET /api/v1/policies takes no query
filter at all (only pagination) — use "policies search" for
query/display-name/policy-type filtering, or to include soft-deleted
policies.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newPoliciesClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
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
			// GET /api/v1/policies takes snake_case query params
			// (page_size/page_token), unlike the search/create bodies below
			// it (which are ordinary camelCase protojson) — verified against
			// the platform source's generated apigw decoder.
			params := map[string]string{
				"page_size": strconv.Itoa(pageSize),
			}
			if pageToken != "" {
				params["page_token"] = pageToken
			}

			data, err := c.Get(cmd.Context(), "/api/v1/policies", params)
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

			// A page can come back short (even empty) while nextPageToken is
			// still set — server-side scope filtering happens after the page
			// is fetched from storage. Keep paging on the token, not on
			// whether this page had rows.
			if resp.NextPageToken == "" || manualPaging {
				break
			}
			pageToken = resp.NextPageToken
		}

		return nil
	},
}

func init() {
	// One of the endpoints behind the shared --page-size caveat: it
	// over-fetches to absorb server-side filtering and does not trim back
	// (measured live, page_size=10 returned 12 rows). Cursor-following and
	// --limit are unaffected.
	addPaginationFlags(policiesListCmd)
	policiesCmd.AddCommand(policiesListCmd)
}
