package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

func connectorPath(appID, connectorID string) string {
	return client.Path("/api/v1/apps/%s/connectors/%s", appID, connectorID)
}

var connectorsGetCmd = &cobra.Command{
	Use:   "get <connector-id>",
	Short: "Get a single connector (pretty JSON)",
	Long: `Get a single connector by its ID.

Output is the connector itself, not the API's "connectorView" wrapper; the id
is .id, and .status.status is the sync status. "config" is printed as the API
returns it.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		return getConnectorResource(cmd, connectorPath(appID, args[0]), func(data []byte) error {
			return writeResource(cmd, data, "id")
		})
	},
}

var connectorsSyncStatusCmd = &cobra.Command{
	Use:   "sync-status <connector-id>",
	Short: "Show a connector's current sync progress (pretty JSON)",
	Long: `Show a connector's current sync progress: status, step, live object counts,
the last error and whether syncing is paused (syncDisabledReason).

Output is the API's "status" object itself.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		path := connectorPath(appID, args[0]) + "/sync_diagnostics/status"
		return getConnectorResource(cmd, path, func(data []byte) error {
			return writeNamedResource(cmd, data, "status")
		})
	},
}

func getConnectorResource(cmd *cobra.Command, path string, write func([]byte) error) error {
	baseURL, err := GetBaseURL()
	if err != nil {
		return err
	}
	c, err := newClient(cmd, baseURL)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	data, err := c.Get(cmd.Context(), path, nil)
	if err != nil {
		return fmt.Errorf("API error: %w", err)
	}
	return write(data)
}

func init() {
	for _, c := range []*cobra.Command{connectorsGetCmd, connectorsSyncStatusCmd} {
		c.Flags().String("app-id", "", "Application ID")
		markRequired(c, "app-id")
		connectorsCmd.AddCommand(c)
	}
}
