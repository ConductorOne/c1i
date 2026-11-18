package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var connectorsDeleteCmd = &cobra.Command{
	Use:   "delete <connector-id>",
	Short: "Delete a connector",
	Long: `Delete a connector.

c1i first reads the connector, on --dry-run too: an unknown id exits 4 and
nothing is sent. A connector that backs an MCP server exits 2, nothing sent:
deleting it here removed the server but left its toolsets and their
entitlements live, so use "c1i mcp servers delete", which removes those too.

In the run checked, a deleted connector stayed readable with "connectors get"
(.deletedAt set), and deleting it again exited 0.

Prints the API's response, which is {}; exit 0 is the success signal. Honors
--dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		path := connectorPath(appID, args[0])
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: path},
			withPreRead(connectorNotMCPCheck(path, args[0])))
	},
}

// mcpConfigPackage marks a connector config type owned by the MCP server API.
const mcpConfigPackage = "/c1.models.ai_governance."

// connectorNotMCPCheck reads the connector and refuses one whose config type
// belongs to the MCP server API.
func connectorNotMCPCheck(path, connectorID string) preRead {
	return preRead{Run: func(ctx context.Context, c *client.Client, _ *mutation) ([]resolvedID, error) {
		data, err := c.Get(ctx, path, nil)
		if err != nil {
			return nil, fmt.Errorf("reading connector %q: %w", connectorID, err)
		}
		var resp struct {
			ConnectorView *struct {
				Connector *struct {
					Config map[string]any `json:"config"`
				} `json:"connector"`
			} `json:"connectorView"`
		}
		if err := json.Unmarshal(data, &resp); err != nil || resp.ConnectorView == nil || resp.ConnectorView.Connector == nil {
			return nil, &nonJSONResponseError{fmt.Errorf("reading connector %q: the response has no connectorView.connector", connectorID)}
		}
		if t, _ := resp.ConnectorView.Connector.Config["@type"].(string); strings.Contains(t, mcpConfigPackage) {
			return nil, &usageError{fmt.Errorf("connector %q backs an MCP server (config %s); delete it with \"c1i mcp servers delete %s --app-id <app-id>\", which also removes its toolsets", connectorID, t, connectorID)}
		}
		return nil, nil
	}}
}

func init() {
	connectorsDeleteCmd.Flags().String("app-id", "", "Application ID")
	markRequired(connectorsDeleteCmd, "app-id")
	markMutating(connectorsDeleteCmd)
	connectorsCmd.AddCommand(connectorsDeleteCmd)
}
