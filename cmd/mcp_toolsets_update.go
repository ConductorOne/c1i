package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpToolsetsUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update an MCP toolset's display name and/or description (pretty JSON)",
	Long: `Update editable fields on a toolset (display_name, description).

The update_mask is derived from which flags you set: pass --display-name to
update only the name, --description for only the description, both for both.
Editing the AppEntitlement created behind the toolset is not supported here.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id", "connector-id", "id"); err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := client.New(cmd.Context(), baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		appID, _ := cmd.Flags().GetString("app-id")
		connectorID, _ := cmd.Flags().GetString("connector-id")
		id, _ := cmd.Flags().GetString("id")

		profile := map[string]any{
			"id":          id,
			"appId":       appID,
			"connectorId": connectorID,
		}
		var paths []string
		if cmd.Flags().Changed("display-name") {
			v, _ := cmd.Flags().GetString("display-name")
			profile["displayName"] = v
			// FieldMask paths use the proto3-JSON (camelCase) field name, matching
			// the body key and the convention in accounts set-owner ("identityUserId").
			paths = append(paths, "displayName")
		}
		if cmd.Flags().Changed("description") {
			v, _ := cmd.Flags().GetString("description")
			profile["description"] = v
			paths = append(paths, "description")
		}
		if len(paths) == 0 {
			return fmt.Errorf("nothing to update: pass --display-name and/or --description")
		}

		body := map[string]any{
			"profile":    profile,
			"updateMask": strings.Join(paths, ","),
		}

		path := client.Path("/api/v1/apps/%s/connectors/%s/mcp_toolsets/%s", appID, connectorID, id)
		data, err := c.Post(cmd.Context(), path, body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		var pretty bytes.Buffer
		if err := json.Indent(&pretty, data, "", "  "); err != nil {
			return fmt.Errorf("failed to pretty-print response: %w", err)
		}
		_, _ = pretty.WriteTo(cmd.OutOrStdout())
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		return nil
	},
}

func init() {
	mcpToolsetsUpdateCmd.Flags().String("app-id", "", "Application ID")
	mcpToolsetsUpdateCmd.Flags().String("connector-id", "", "Connector ID")
	mcpToolsetsUpdateCmd.Flags().String("id", "", "MCP toolset ID")
	mcpToolsetsUpdateCmd.Flags().String("display-name", "", "New display name (omit to leave unchanged)")
	mcpToolsetsUpdateCmd.Flags().String("description", "", "New description (omit to leave unchanged)")
	markRequired(mcpToolsetsUpdateCmd, "app-id", "connector-id", "id")
	mcpToolsetsCmd.AddCommand(mcpToolsetsUpdateCmd)
}
