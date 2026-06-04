package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpToolsetsRequestableConnectorsCmd = &cobra.Command{
	Use:   "requestable-connectors",
	Short: "List connectors with toolsets the given user can request (NDJSON output)",
	Long: `For a user, return the (app_id, connector_id) pairs that have at least one
MCP toolset the user can currently request access to.

Unlike most other "list" commands this endpoint is not paginated server-side
— it returns the full set in one response.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "user-id"); err != nil {
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

		userID, _ := cmd.Flags().GetString("user-id")

		path := fmt.Sprintf("/api/v1/users/%s/mcp_toolsets/requestable_connectors", userID)
		data, err := c.Get(cmd.Context(), path, nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		var resp struct {
			Connectors []struct {
				AppID       string `json:"appId"`
				ConnectorID string `json:"connectorId"`
			} `json:"connectors"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		enc := json.NewEncoder(cmd.OutOrStdout())
		for _, cn := range resp.Connectors {
			_ = enc.Encode(map[string]string{
				"app_id":       cn.AppID,
				"connector_id": cn.ConnectorID,
			})
		}
		return nil
	},
}

func init() {
	mcpToolsetsRequestableConnectorsCmd.Flags().String("user-id", "", "User ID")
	markRequired(mcpToolsetsRequestableConnectorsCmd, "user-id")
	mcpToolsetsCmd.AddCommand(mcpToolsetsRequestableConnectorsCmd)
}
