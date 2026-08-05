package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpToolsetsRequestableConnectorsCmd = &cobra.Command{
	Use:   "requestable-connectors <user-id>",
	Short: "List connectors with toolsets the given user can request (NDJSON output)",
	Long: `For a user, return the (app_id, connector_id) pairs that have at least one
MCP toolset the user can currently request access to.

Unlike most other "list" commands this endpoint is not paginated server-side
— it returns the full set in one response.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		userID := args[0]

		path := client.Path("/api/v1/users/%s/mcp_toolsets/requestable_connectors", userID)
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

		enc := newEmitter(cmd.OutOrStdout())
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
	mcpToolsetsCmd.AddCommand(mcpToolsetsRequestableConnectorsCmd)
}
