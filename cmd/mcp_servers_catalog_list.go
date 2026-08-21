package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

var mcpServersCatalogListCmd = &cobra.Command{
	Use:   "list",
	Short: "List MCP server catalog entries (NDJSON output)",
	Long: `List the HOSTED MCP server catalog entries. Filter by free-text --query
(matches display_name). Scope/maturity enum filters are not exposed over REST
yet (the SDK can't encode them) — page through and filter client-side if needed.

Rows include service_name, base_url, default_tool_prefix, and stable so
near-duplicate entries for the same service (e.g. "Slack" vs "Slack API" — one
is the vendor's hosted MCP endpoint, the other a thin wrapper over its plain
REST API) can be told apart without a round trip to "get". They also include
required_scope_count/optional_scope_count, a summary of that entry's OAuth
scope tiering (see "catalog get" for details and caveats).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newListClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		query, _ := cmd.Flags().GetString("query")
		requestedPageSize := clampPageSize(getIntFlag(cmd, "page-size"))
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		enc := newEmitter(cmd)
		for !limitReached(enc.Written(), limit) {
			pageSize := effectivePageSize(requestedPageSize, limit, enc.Written())
			params := map[string]string{
				"page_size": strconv.Itoa(pageSize),
			}
			if pageToken != "" {
				params["page_token"] = pageToken
			}
			if query != "" {
				params["query"] = query
			}

			data, err := c.Get(cmd.Context(), "/api/v1/mcp_server_catalog", params)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List          []catalogEntryView `json:"list"`
				NextPageToken string             `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, e := range resp.List {
				_ = enc.Encode(catalogEntryRow(e))
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
	mcpServersCatalogListCmd.Flags().String("query", "", "Filter catalog entries by display name")
	mcpServersCatalogListCmd.Flags().Int("page-size", 50, "Results per page (max 100)")
	mcpServersCatalogListCmd.Flags().String("page-token", "", "Pagination cursor")
	addLimitFlag(mcpServersCatalogListCmd)
	mcpServersCatalogCmd.AddCommand(mcpServersCatalogListCmd)
}
