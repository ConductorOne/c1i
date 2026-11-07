package cmd

import (
	"fmt"
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpBindingsDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Unbind one or more MCP tools from a toolset",
	Long: `Remove one or more MCP tools (--tool-id, repeatable) from a toolset.

Uses the POST .../tool_bindings/delete action route because the tool IDs travel
in the request body; HTTP DELETE doesn't reliably support that.

Prints the API's response, which is {}; exit 0 is the success signal. Honors
--dry-run.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id", "connector-id", "toolset-id"); err != nil {
			return err
		}

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

		path := client.Path("/api/v1/apps/%s/connectors/%s/mcp_toolsets/%s/tool_bindings/delete", appID, connectorID, toolsetID)
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: body})
	},
}

func init() {
	mcpBindingsDeleteCmd.Flags().String("app-id", "", "Application ID")
	mcpBindingsDeleteCmd.Flags().String("connector-id", "", "Connector ID")
	mcpBindingsDeleteCmd.Flags().String("toolset-id", "", "MCP toolset (access profile) ID")
	addRepeatableStringFlag(mcpBindingsDeleteCmd, "tool-id", "MCP tool ID to unbind (repeatable; max 100)")
	markRequired(mcpBindingsDeleteCmd, "app-id", "connector-id", "toolset-id", "tool-id")
	markMutating(mcpBindingsDeleteCmd)
	mcpBindingsCmd.AddCommand(mcpBindingsDeleteCmd)
}
