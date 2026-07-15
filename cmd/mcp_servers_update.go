package cmd

import (
	"fmt"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpServersUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update an MCP server's editable metadata (pretty JSON)",
	Long: `Update editable metadata on an MCP server. The update_mask is derived from
which flags you set: pass --display-name to change only the name, and so on.

Updatable fields: display_name, description, data_sensitivity, tool_prefix,
require_tool_approval. Auth config and URL are NOT updatable here — use
"c1i mcp servers update-credentials" for those.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id", "connector-id"); err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		appID, _ := cmd.Flags().GetString("app-id")
		connectorID, _ := cmd.Flags().GetString("connector-id")

		server := map[string]any{
			"appId":       appID,
			"connectorId": connectorID,
		}
		var paths []string
		if cmd.Flags().Changed("display-name") {
			v, _ := cmd.Flags().GetString("display-name")
			server["displayName"] = v
			paths = append(paths, "displayName")
		}
		if cmd.Flags().Changed("description") {
			v, _ := cmd.Flags().GetString("description")
			server["description"] = v
			paths = append(paths, "description")
		}
		if cmd.Flags().Changed("data-sensitivity") {
			v, _ := cmd.Flags().GetString("data-sensitivity")
			server["dataSensitivity"] = mapDataSensitivity(v)
			paths = append(paths, "dataSensitivity")
		}
		if cmd.Flags().Changed("tool-prefix") {
			v, _ := cmd.Flags().GetString("tool-prefix")
			server["toolPrefix"] = v
			paths = append(paths, "toolPrefix")
		}
		if cmd.Flags().Changed("require-tool-approval") {
			on, _ := cmd.Flags().GetBool("require-tool-approval")
			if on {
				server["requireToolApproval"] = "OPTIONAL_BOOL_TRUE"
			} else {
				server["requireToolApproval"] = "OPTIONAL_BOOL_FALSE"
			}
			paths = append(paths, "requireToolApproval")
		}
		if len(paths) == 0 {
			return fmt.Errorf("nothing to update: pass at least one of --display-name, --description, --data-sensitivity, --tool-prefix, --require-tool-approval")
		}

		body := map[string]any{
			"appId":       appID,
			"connectorId": connectorID,
			"mcpServer":   server,
			"updateMask":  strings.Join(paths, ","),
		}

		path := client.Path("/api/v1/apps/%s/mcp_servers/%s", appID, connectorID)
		if dryRunActive() {
			return printDryRun(cmd, "POST", path, body)
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Post(cmd.Context(), path, body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeRawObject(cmd, data)
	},
}

func init() {
	mcpServersUpdateCmd.Flags().String("app-id", "", "Application ID")
	mcpServersUpdateCmd.Flags().String("connector-id", "", "MCP server (connector) ID")
	mcpServersUpdateCmd.Flags().String("display-name", "", "New display name")
	mcpServersUpdateCmd.Flags().String("description", "", "New description")
	mcpServersUpdateCmd.Flags().String("data-sensitivity", "", "New data sensitivity: public, internal, confidential, restricted")
	mcpServersUpdateCmd.Flags().String("tool-prefix", "", "New tool-name prefix")
	mcpServersUpdateCmd.Flags().Bool("require-tool-approval", false, "Per-server override for tool auto-approval")
	markRequired(mcpServersUpdateCmd, "app-id", "connector-id")
	mcpServersCmd.AddCommand(mcpServersUpdateCmd)
}
