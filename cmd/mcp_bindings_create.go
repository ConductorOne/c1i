package cmd

import (
	"fmt"
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpBindingsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Bind one or more MCP tools to a toolset (pretty JSON)",
	Long: `Bind one or more MCP tools (--tool-id, repeatable) to a toolset. Prints the
API's response as returned: {"bindings":[{"mcpToolId":…}]}, with no id of its own.`,
	RunE: func(cmd *cobra.Command, args []string) error {

		appID, _ := cmd.Flags().GetString("app-id")
		connectorID, _ := cmd.Flags().GetString("connector-id")
		toolsetID, _ := cmd.Flags().GetString("toolset-id")
		toolIDs, err := repeatableStringFlag(cmd, "tool-id")
		if err != nil {
			return err
		}
		if len(toolIDs) == 0 {
			return &usageError{fmt.Errorf("flag --tool-id requires at least one value")}
		}

		body := map[string]any{
			"appId":           appID,
			"connectorId":     connectorID,
			"accessProfileId": toolsetID,
			"mcpToolIds":      toolIDs,
		}

		path := client.Path("/api/v1/apps/%s/connectors/%s/mcp_toolsets/%s/tool_bindings", appID, connectorID, toolsetID)
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: body, Unwrap: byID("id")})
	},
}

func init() {
	mcpBindingsCreateCmd.Flags().String("app-id", "", "Application ID")
	mcpBindingsCreateCmd.Flags().String("connector-id", "", "Connector ID")
	mcpBindingsCreateCmd.Flags().String("toolset-id", "", "MCP toolset (access profile) ID")
	addRepeatableStringFlag(mcpBindingsCreateCmd, "tool-id", "MCP tool ID to bind (repeatable; max 100)")
	markRequired(mcpBindingsCreateCmd, "app-id", "connector-id", "toolset-id", "tool-id")
	markMutating(mcpBindingsCreateCmd)
	mcpBindingsCmd.AddCommand(mcpBindingsCreateCmd)
}
