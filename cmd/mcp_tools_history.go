package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpToolsHistoryCmd = &cobra.Command{
	Use:   "history <tool-id>",
	Short: "Stream the change history for a single MCP tool (NDJSON output)",
	Long: `One NDJSON record per historical version of the tool, newest-first as
the server returns them. The record carries both the full tool snapshot at
that point in time and the change-history envelope (actor, change_kind,
created_at, trace_id, syslog_event_id, annotations).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id", "connector-id"); err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newListClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		appID, _ := cmd.Flags().GetString("app-id")
		connectorID, _ := cmd.Flags().GetString("connector-id")
		id := args[0]
		requestedPageSize := clampHistoryPageSize(getIntFlag(cmd, "page-size"))
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		enc := newEmitter(cmd)
		for !limitReached(enc.Written(), limit) {
			pageSize := requestedPageSize
			if !enc.Filtered() {
				pageSize = effectivePageSize(requestedPageSize, limit, enc.Written())
			}
			params := map[string]string{
				"page_size": strconv.Itoa(pageSize),
			}
			if pageToken != "" {
				params["page_token"] = pageToken
			}

			path := client.Path("/api/v1/apps/%s/connectors/%s/mcp_tools/%s/history", appID, connectorID, id)
			data, err := c.Get(cmd.Context(), path, params)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List          []json.RawMessage `json:"list"`
				NextPageToken string            `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, entry := range resp.List {
				_ = enc.Encode(json.RawMessage(entry))
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

// clampHistoryPageSize caps to 200, the history endpoint's higher per-page
// limit (vs. the default 100 used elsewhere). The shared clampPageSize cap
// of 100 would silently truncate batches the API would otherwise accept.
func clampHistoryPageSize(n int) int {
	const maxHistoryPageSize = 200
	if n > maxHistoryPageSize {
		return maxHistoryPageSize
	}
	return n
}

func init() {
	mcpToolsHistoryCmd.Flags().String("app-id", "", "Application ID")
	mcpToolsHistoryCmd.Flags().String("connector-id", "", "Connector ID")
	mcpToolsHistoryCmd.Flags().Int("page-size", 50, "Results per page (max 200)")
	mcpToolsHistoryCmd.Flags().String("page-token", "", "Pagination cursor")
	markRequired(mcpToolsHistoryCmd, "app-id", "connector-id")
	addLimitFlag(mcpToolsHistoryCmd)
	mcpToolsCmd.AddCommand(mcpToolsHistoryCmd)
}
