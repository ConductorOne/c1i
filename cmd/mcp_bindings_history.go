package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpBindingsHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "Stream binding-change history scoped to a toolset OR a tool (NDJSON output)",
	Long: `Stream the binding-change history for either:

  - all tools bound to one toolset (--toolset-id), or
  - all toolsets containing one tool (--tool-id).

Exactly one of --toolset-id / --tool-id is required. Records are emitted
newest-first as the server returns them. Each NDJSON record carries a
list_history metadata envelope plus the items added or removed in that
transaction.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id", "connector-id"); err != nil {
			return err
		}
		toolsetID, _ := cmd.Flags().GetString("toolset-id")
		toolID, _ := cmd.Flags().GetString("tool-id")
		if (toolsetID == "") == (toolID == "") {
			return fmt.Errorf("exactly one of --toolset-id or --tool-id is required")
		}
		// Read-only command — guard against silent over-fetch from an empty
		// path segment that would still pattern-validate as "missing".
		if cmd.Flags().Changed("toolset-id") && toolsetID == "" {
			return fmt.Errorf("flag --toolset-id requires a non-empty value")
		}
		if cmd.Flags().Changed("tool-id") && toolID == "" {
			return fmt.Errorf("flag --tool-id requires a non-empty value")
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		appID, _ := cmd.Flags().GetString("app-id")
		connectorID, _ := cmd.Flags().GetString("connector-id")
		requestedPageSize := clampHistoryPageSize(getIntFlag(cmd, "page-size"))
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		var basePath string
		if toolsetID != "" {
			basePath = client.Path("/api/v1/apps/%s/connectors/%s/mcp_toolsets/%s/tool_bindings/history", appID, connectorID, toolsetID)
		} else {
			basePath = client.Path("/api/v1/apps/%s/connectors/%s/tool_bindings/by_tool/%s/history", appID, connectorID, toolID)
		}

		enc := newEmitter(cmd.OutOrStdout())
		emitted := 0
		for !limitReached(emitted, limit) {
			pageSize := effectivePageSize(requestedPageSize, limit, emitted)
			params := map[string]string{
				"page_size": strconv.Itoa(pageSize),
			}
			if pageToken != "" {
				params["page_token"] = pageToken
			}

			data, err := c.Get(cmd.Context(), basePath, params)
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
				emitted++
				if limitReached(emitted, limit) {
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
	mcpBindingsHistoryCmd.Flags().String("app-id", "", "Application ID")
	mcpBindingsHistoryCmd.Flags().String("connector-id", "", "Connector ID")
	mcpBindingsHistoryCmd.Flags().String("toolset-id", "", "MCP toolset (access profile) ID")
	mcpBindingsHistoryCmd.Flags().String("tool-id", "", "MCP tool ID")
	mcpBindingsHistoryCmd.Flags().Int("page-size", 50, "Results per page (max 200)")
	mcpBindingsHistoryCmd.Flags().String("page-token", "", "Pagination cursor")
	markRequired(mcpBindingsHistoryCmd, "app-id", "connector-id")
	addLimitFlag(mcpBindingsHistoryCmd)
	mcpBindingsCmd.AddCommand(mcpBindingsHistoryCmd)
}
