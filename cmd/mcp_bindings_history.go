package cmd

import (
	"encoding/json"
	"fmt"

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
		toolsetID, _ := cmd.Flags().GetString("toolset-id")
		toolID, _ := cmd.Flags().GetString("tool-id")
		if (toolsetID == "") == (toolID == "") {
			return &usageError{fmt.Errorf("exactly one of --toolset-id or --tool-id is required")}
		}

		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		appID, _ := cmd.Flags().GetString("app-id")
		connectorID, _ := cmd.Flags().GetString("connector-id")
		var basePath string
		if toolsetID != "" {
			basePath = client.Path("/api/v1/apps/%s/connectors/%s/mcp_toolsets/%s/tool_bindings/history", appID, connectorID, toolsetID)
		} else {
			basePath = client.Path("/api/v1/apps/%s/connectors/%s/tool_bindings/by_tool/%s/history", appID, connectorID, toolID)
		}
		// Rows carry no id of their own, so only the token and idle-page checks apply.
		return listPages(cmd, c, pageSpec[json.RawMessage]{
			Route: "GET " + basePath,
			Fetch: getPage(cmd.Context(), basePath, nil),
			Parse: parseList[json.RawMessage],
		})
	},
}

func init() {
	mcpBindingsHistoryCmd.Flags().String("app-id", "", "Application ID")
	mcpBindingsHistoryCmd.Flags().String("connector-id", "", "Connector ID")
	mcpBindingsHistoryCmd.Flags().String("toolset-id", "", "MCP toolset (access profile) ID")
	mcpBindingsHistoryCmd.Flags().String("tool-id", "", "MCP tool ID")
	addPaginationFlagsWithMax(mcpBindingsHistoryCmd, defaultPageSize, maxHistoryPageSize)
	markRequired(mcpBindingsHistoryCmd, "app-id", "connector-id")
	mcpBindingsCmd.AddCommand(mcpBindingsHistoryCmd)
}
