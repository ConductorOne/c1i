package cmd

import (
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var mcpServersCmd = &cobra.Command{
	Use:   "servers",
	Short: "Manage MCP servers (register, configure, and inspect)",
	Long: `Register, configure, and inspect the MCP servers under a C1 app.

An MCP server is modeled as a Connector under an App (identified by app_id +
connector_id). HOSTED servers run inside C1 from a catalog impl; EXTERNAL
servers point at a third-party MCP URL. Auth secrets are sent in the request
body and sealed server-side — reads only ever return "*_configured" booleans.

"resync-tools" and "test-connection" are EXTERNAL-only; both return a 400 on a
HOSTED server.

Subcommands:
  list / get / search   - Inspect servers registered under an app
  register              - Register a new HOSTED or EXTERNAL server
  update                - Edit metadata (display name, description, etc.)
  update-credentials    - Replace the auth config / config fields
  delete                - Soft-delete a server
  resync-tools          - Re-run tool discovery for the caller's credential
  test-connection       - Probe an EXTERNAL server's reachability
  discover-oidc         - Fetch an issuer's OIDC discovery document
  catalog list / get    - Browse the HOSTED-server catalog
  connections list      - List per-user (passthrough) connections for the caller

After registering, tools start in PENDING_REVIEW — approve them with
"c1i mcp tools approve" before the gateway will proxy calls.`,
}

func init() {
	mcpCmd.AddCommand(mcpServersCmd)
}

// mapServerType translates a user-friendly --type value to the API enum.
// Input is case-insensitive.
func mapServerType(s string) string {
	switch strings.ToLower(s) {
	case "hosted":
		return "MCP_SERVER_TYPE_HOSTED"
	case "external":
		return "MCP_SERVER_TYPE_EXTERNAL"
	default:
		return s
	}
}

// mapDataSensitivity translates a user-friendly --data-sensitivity value to the
// API enum. Input is case-insensitive.
func mapDataSensitivity(s string) string {
	switch strings.ToLower(s) {
	case "public":
		return "MCP_SERVER_DATA_SENSITIVITY_PUBLIC"
	case "internal":
		return "MCP_SERVER_DATA_SENSITIVITY_INTERNAL"
	case "confidential":
		return "MCP_SERVER_DATA_SENSITIVITY_CONFIDENTIAL"
	case "restricted":
		return "MCP_SERVER_DATA_SENSITIVITY_RESTRICTED"
	default:
		return s
	}
}

// mapTransportType translates a user-friendly --transport value to the API
// enum. Input is case-insensitive.
func mapTransportType(s string) string {
	switch strings.ToLower(s) {
	case "streamable-http", "streamable_http", "http":
		return "MCP_SERVER_TRANSPORT_TYPE_STREAMABLE_HTTP"
	case "sse":
		return "MCP_SERVER_TRANSPORT_TYPE_SSE"
	default:
		return s
	}
}

// mapTokenSharing translates a user-friendly --token-sharing value to the API
// enum. Input is case-insensitive.
func mapTokenSharing(s string) string {
	switch strings.ToLower(s) {
	case "shared":
		return "MCP_SERVER_TOKEN_SHARING_SHARED"
	case "per-user", "per_user", "peruser":
		return "MCP_SERVER_TOKEN_SHARING_PER_USER"
	default:
		return s
	}
}

// flexInt64 unmarshals an int64 that the API may encode either as a JSON
// number or, per canonical proto3 JSON for 64-bit ints, as a quoted string.
// Accepting both keeps the tool-count parsing from breaking on either form.
type flexInt64 int64

func (f *flexInt64) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return err
	}
	*f = flexInt64(n)
	return nil
}

// nilIfEmpty converts an empty string to untyped nil so an absent value
// round-trips as JSON null, not "" — "" is truthy in jq, so `jq
// 'select(.field)'` would otherwise match every row. See cmd/policies.go's
// policyRow for the convention this codifies.
func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// serverView is the subset of MCPServerView we surface in NDJSON list rows.
// The full object (with all read-only *_configured booleans and OAuth2
// details) is available via `get` / `--fields` on the pretty-JSON commands.
type serverView struct {
	ConnectorID        string `json:"connectorId"`
	AppID              string `json:"appId"`
	DisplayName        string `json:"displayName"`
	Description        string `json:"description"`
	ServerType         string `json:"serverType"`
	DataSensitivity    string `json:"dataSensitivity"`
	AuthMethod         string `json:"authMethod"`
	MCPServerCatalogID string `json:"mcpServerCatalogId"`
	ToolPrefix         string `json:"toolPrefix"`
	EndpointURL        string `json:"endpointUrl"`
	TokenSharing       string `json:"tokenSharing"`
	CreatedAt          string `json:"createdAt"`
	// LastCalledAt is populated by SearchWithToolCount only when
	// --include-last-called-at is set; empty otherwise.
	LastCalledAt string `json:"lastCalledAt"`
}

func serverRow(s serverView) map[string]any {
	return map[string]any{
		"connector_id":          s.ConnectorID,
		"app_id":                s.AppID,
		"display_name":          s.DisplayName,
		"description":           s.Description,
		"server_type":           s.ServerType,
		"data_sensitivity":      s.DataSensitivity,
		"auth_method":           s.AuthMethod,
		"mcp_server_catalog_id": s.MCPServerCatalogID,
		"tool_prefix":           s.ToolPrefix,
		"endpoint_url":          s.EndpointURL,
		"token_sharing":         s.TokenSharing,
		"created_at":            s.CreatedAt,
	}
}

// serverCountRow is a serverRow with the per-server tool count appended, used
// by `search` (which wraps each view in {mcpServer, toolCount}). last_called_at
// is included only when the view carries it (i.e. --include-last-called-at),
// so the column doesn't appear as a misleading always-empty field otherwise.
func serverCountRow(s serverView, toolCount int64) map[string]any {
	row := serverRow(s)
	row["tool_count"] = toolCount
	if s.LastCalledAt != "" {
		row["last_called_at"] = s.LastCalledAt
	}
	return row
}
