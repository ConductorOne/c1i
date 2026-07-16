package cmd

import (
	"strconv"

	"github.com/spf13/cobra"
)

var mcpServersConnectionsCmd = &cobra.Command{
	Use:   "connections",
	Short: "Inspect the calling user's per-user MCP connections",
	Long: `List the passthrough-mode (per-user) MCP servers the calling user can
connect to, along with whether the user currently has an active credential.`,
}

func init() {
	mcpServersCmd.AddCommand(mcpServersConnectionsCmd)
}

// connectionView is the subset of MCPConnectionView surfaced in list rows.
type connectionView struct {
	ConnectorID       string `json:"connectorId"`
	AppID             string `json:"appId"`
	DisplayName       string `json:"displayName"`
	ServerType        string `json:"serverType"`
	AuthMethod        string `json:"authMethod"`
	Connected         bool   `json:"connected"`
	AuthorizedAsEmail string `json:"authorizedAsEmail"`
	AuthorizedAsName  string `json:"authorizedAsName"`
	ConnectedAt       string `json:"connectedAt"`
}

func connectionRow(v connectionView) map[string]string {
	return map[string]string{
		"connector_id":        v.ConnectorID,
		"app_id":              v.AppID,
		"display_name":        v.DisplayName,
		"server_type":         v.ServerType,
		"auth_method":         v.AuthMethod,
		"connected":           strconv.FormatBool(v.Connected),
		"authorized_as_email": v.AuthorizedAsEmail,
		"authorized_as_name":  v.AuthorizedAsName,
		"connected_at":        v.ConnectedAt,
	}
}
