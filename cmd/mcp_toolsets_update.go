package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpToolsetsUpdateSpec = &updateSpec{
	fields: []updateField{strUpdate("display-name", "displayName"), strUpdate("description", "description")},
	scope:  []string{"app-id", "connector-id"},
}

var mcpToolsetsUpdateCmd = &cobra.Command{
	Use:   "update <toolset-id>",
	Short: "Update an MCP toolset's display name and description (pretty JSON)",
	Long: `Update editable fields on a toolset (display_name, description).

The update_mask is derived from which flags you set. The server requires
--display-name on every update: --description alone 400s with
"DisplayName value length must be between 1 and 255 runes" (exit 2,
applied:false). To change only the description, pass the current name too.
Editing the AppEntitlement created behind the toolset is not supported here.

A blank --display-name (empty, whitespace or invisible-only) exits 2,
nothing sent. Sent as is, the server 400s "" ("value length must be between
1 and 255 runes") but stores "  " verbatim.

Prints the updated toolset, unwrapped like "mcp toolsets get".`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		connectorID, _ := cmd.Flags().GetString("connector-id")
		id := args[0]

		profile := map[string]any{
			"id":          id,
			"appId":       appID,
			"connectorId": connectorID,
		}
		mask, err := mcpToolsetsUpdateSpec.build(cmd, profile)
		if err != nil {
			return err
		}

		path := client.Path("/api/v1/apps/%s/connectors/%s/mcp_toolsets/%s", appID, connectorID, id)
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: wrapUpdate("profile", profile, mask)})
	},
}

func init() {
	mcpToolsetsUpdateCmd.Flags().String("app-id", "", "Application ID")
	mcpToolsetsUpdateCmd.Flags().String("connector-id", "", "Connector ID")
	mcpToolsetsUpdateCmd.Flags().String("display-name", "", "New display name (the server requires it on every update; "+displayNameRule+")")
	mcpToolsetsUpdateCmd.Flags().String("description", "", `New description; "" is sent as an empty value (pass --display-name too; the server requires it)`)
	allowEmpty(mcpToolsetsUpdateCmd, emptyIsSent, "description")
	markRequired(mcpToolsetsUpdateCmd, "app-id", "connector-id")
	registerUpdateSpec(mcpToolsetsUpdateCmd, mcpToolsetsUpdateSpec)
	markMutating(mcpToolsetsUpdateCmd)
	mcpToolsetsCmd.AddCommand(mcpToolsetsUpdateCmd)
}
