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

// resolveMCPServerCatalogID fetches a hosted server's stored catalog id. A
// hosted config must always carry mcp_server_catalog_id (proto min_len:1), even
// when the caller is only rotating auth, so update-credentials resolves it when
// --catalog-id wasn't supplied.
func resolveMCPServerCatalogID(ctx context.Context, c *client.Client, appID, connectorID string) (string, error) {
	data, err := c.Get(ctx, client.Path("/api/v1/apps/%s/mcp_servers/%s", appID, connectorID), nil)
	if err != nil {
		return "", fmt.Errorf("resolving catalog id (pass --catalog-id to skip the lookup): %w", err)
	}
	var resp struct {
		MCPServer struct {
			McpServerCatalogID string `json:"mcpServerCatalogId"`
		} `json:"mcpServer"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("parsing server response: %w", err)
	}
	if resp.MCPServer.McpServerCatalogID == "" {
		return "", &usageError{fmt.Errorf("server %s has no catalog id; pass --catalog-id explicitly", connectorID)}
	}
	return resp.MCPServer.McpServerCatalogID, nil
}

var mcpServersUpdateCredentialsCmd = &cobra.Command{
	Use:   "update-credentials <connector-id>",
	Short: "Replace an MCP server's auth config / config fields (pretty JSON)",
	Long: `Replace the auth config and/or extra config fields on an existing MCP server.
Reuses the same sealing + validation path as register, so auth secrets are
re-sealed server-side.

Pick the config shape with --type (must match the server): hosted or external.
Supply the new config with the same convenience auth flags as register, or the
full --hosted-config-file / --external-config-file JSON.

The update_mask is derived from the fields you supply — e.g. --auth bearer-token
masks "bearerToken"; --server-url masks "url". Pass --update-mask with comma-separated
proto field paths to override. Note the mask uses the auth oneof case name
("bearerToken", "customHeader", "basicAuth", "oauth2", "none"), not the
"hostedConfig"/"externalConfig" wrapper.

Companion values are required for the selected arm, with two exceptions. With
--token-sharing per-user the secret is left to each user. With an explicit
--update-mask, only the arm fields the mask names are required (e.g.
--auth basic-auth --basic-auth-password NEW --update-mask basicAuth.password
rotates the password and leaves the username stored); a coarse path such as
"basicAuth" names the whole arm and still requires all of it. Omitted fields
must already exist on the server. Token sharing cannot be read back, so repeat
--token-sharing per-user when rotating a stored per-user server without its
secret.

Prints the updated server, unwrapped like "mcp servers get" (id: .connectorId).
For a hosted server without --catalog-id, c1i first looks the catalog id up
(also on --dry-run) and reports "Resolved catalog id: <id>" on stderr.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		appID, _ := cmd.Flags().GetString("app-id")
		connectorID := args[0]
		typ, _ := cmd.Flags().GetString("type")

		body := map[string]any{
			"appId":       appID,
			"connectorId": connectorID,
		}

		var cfg map[string]any
		var wrapperKey string
		typ, err := serverTypeFlag.parse(typ)
		if err != nil {
			return err
		}
		switch typ {
		case "MCP_SERVER_TYPE_HOSTED":
			cfg, err = buildHostedConfig(cmd)
			wrapperKey = "hostedConfig"
		case "MCP_SERVER_TYPE_EXTERNAL":
			cfg, err = buildExternalConfig(cmd)
			wrapperKey = "externalConfig"
		default:
			return &usageError{fmt.Errorf("invalid --type %q: use hosted or external", typ)}
		}
		if err != nil {
			return err
		}

		mask, _ := cmd.Flags().GetString("update-mask")
		// Guard the destructive case: an empty config with no mask would leave
		// the backend nothing to apply. Require the caller to supply what to set.
		// An explicit --update-mask is trusted as intent.
		if len(cfg) == 0 && mask == "" {
			return &usageError{fmt.Errorf("nothing to update: supply the new config via the auth flags (--auth/--bearer-token/…), --config-field, --server-url, or --%s-config-file", strings.TrimSuffix(wrapperKey, "Config"))}
		}
		// Derive the mask from the fields the caller set, BEFORE the catalog-id
		// injection below. The update_mask must name the proto paths inside the
		// config (e.g. "bearerToken"), NOT the hostedConfig/externalConfig
		// wrapper — see deriveCredentialUpdateMask.
		if mask == "" {
			mask = deriveCredentialUpdateMask(cfg)
		}

		// A hosted config must always carry mcp_server_catalog_id (proto
		// min_len:1) even when only rotating auth. Resolve it when --catalog-id
		// is absent and inject it without adding it to the mask. The lookup
		// authenticates, including on --dry-run.
		var opts []mutationOpt
		if wrapperKey == "hostedConfig" && len(cfg) > 0 {
			if _, ok := cfg["mcpServerCatalogId"]; !ok {
				opts = append(opts, withPreRead(preRead{OnDryRun: true, Run: func(ctx context.Context, c *client.Client, _ *mutation) ([]resolvedID, error) {
					catID, err := resolveMCPServerCatalogID(ctx, c, appID, connectorID)
					if err != nil {
						return nil, err
					}
					cfg["mcpServerCatalogId"] = catID
					return []resolvedID{{Name: "mcpServerCatalogId", Label: "catalog id", ID: catID}}, nil
				}}))
			}
		}

		body[wrapperKey] = cfg
		body["updateMask"] = mask

		path := client.Path("/api/v1/apps/%s/mcp_servers/%s/credentials", appID, connectorID)
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: body, Unwrap: byID("connectorId")}, opts...)
	},
}

func init() {
	mcpServersUpdateCredentialsCmd.Flags().String("app-id", "", "Application ID")
	mcpServersUpdateCredentialsCmd.Flags().String("type", "", "Config shape (must match the server): "+serverTypeFlag.usage())
	// HOSTED config
	mcpServersUpdateCredentialsCmd.Flags().String("catalog-id", "", "Catalog entry ID (HOSTED)")
	mcpServersUpdateCredentialsCmd.Flags().String("source-app-id", "", "Source app ID (HOSTED)")
	addRepeatableStringFlag(mcpServersUpdateCredentialsCmd, "config-field", "Extra config field key=value (HOSTED, repeatable)")
	mcpServersUpdateCredentialsCmd.Flags().String("hosted-config-file", "", "Full hostedConfig JSON (file or \"-\" for stdin)")
	// EXTERNAL config
	mcpServersUpdateCredentialsCmd.Flags().String("server-url", "", "External MCP server URL (EXTERNAL)")
	mcpServersUpdateCredentialsCmd.Flags().String("transport", "", "Transport: "+transportTypeFlag.usage()+" (EXTERNAL)")
	mcpServersUpdateCredentialsCmd.Flags().String("external-config-file", "", "Full externalConfig JSON (file or \"-\" for stdin)")
	// Shared auth
	addAuthFlags(mcpServersUpdateCredentialsCmd)
	mcpServersUpdateCredentialsCmd.Flags().String("update-mask", "", "Comma-separated proto field paths to update (default: derived from the fields you supply)")
	markRequired(mcpServersUpdateCredentialsCmd, "app-id", "type")
	markMutating(mcpServersUpdateCredentialsCmd)
	mcpServersCmd.AddCommand(mcpServersUpdateCredentialsCmd)
}
