package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpServersUpdateSpec = &updateSpec{
	fields: []updateField{
		strUpdate("display-name", "displayName"),
		strUpdate("description", "description"),
		enumUpdate("data-sensitivity", "dataSensitivity", &dataSensitivityFlag),
		strUpdate("tool-prefix", "toolPrefix"),
		optionalBoolUpdate("require-tool-approval", "requireToolApproval"),
	},
	scope: []string{"app-id"},
}

var mcpServersUpdateCmd = &cobra.Command{
	Use:   "update <connector-id>",
	Short: "Update an MCP server's editable metadata (pretty JSON)",
	Long: `Update editable metadata on an MCP server. The update_mask is derived from
which flags you set: pass --display-name to change only the name, and so on.

Updatable fields: display_name, description, data_sensitivity, tool_prefix,
require_tool_approval. Auth config and URL are NOT updatable here — use
"c1i mcp servers update-credentials" for those.

A blank --display-name (empty, whitespace or invisible-only) exits 2,
nothing sent. Sent as is, the server stores "" as an empty name and keeps
"  " verbatim.

Prints the updated server, unwrapped like "mcp servers get" (id: .connectorId).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		connectorID := args[0]

		server := map[string]any{
			"appId":       appID,
			"connectorId": connectorID,
		}
		mask, err := mcpServersUpdateSpec.build(cmd, server, nil)
		if err != nil {
			return err
		}

		body := wrapUpdate("mcpServer", server, mask)
		body["appId"] = appID
		body["connectorId"] = connectorID

		path := client.Path("/api/v1/apps/%s/mcp_servers/%s", appID, connectorID)
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: body, Unwrap: byID("connectorId")})
	},
}

func init() {
	mcpServersUpdateCmd.Flags().String("app-id", "", "Application ID")
	mcpServersUpdateCmd.Flags().String("display-name", "", "New display name (omit to leave unchanged; "+displayNameRule+")")
	mcpServersUpdateCmd.Flags().String("description", "", `New description; "" is sent as an empty value`)
	allowEmpty(mcpServersUpdateCmd, emptyIsSent, "description")
	mcpServersUpdateCmd.Flags().String("data-sensitivity", "", "New data sensitivity: "+dataSensitivityFlag.usage()+`; "" is sent as an empty value`)
	mcpServersUpdateCmd.Flags().String("tool-prefix", "", `New tool-name prefix; "" is sent as an empty value`)
	allowEmpty(mcpServersUpdateCmd, emptyIsSent, "data-sensitivity", "tool-prefix")
	mcpServersUpdateCmd.Flags().Bool("require-tool-approval", false, "Per-server override for tool auto-approval")
	markRequired(mcpServersUpdateCmd, "app-id")
	registerUpdateSpec(mcpServersUpdateCmd, mcpServersUpdateSpec)
	markMutating(mcpServersUpdateCmd)
	mcpServersCmd.AddCommand(mcpServersUpdateCmd)
}
