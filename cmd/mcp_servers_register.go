package cmd

import (
	"fmt"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpServersRegisterCmd = &cobra.Command{
	Use:   "register",
	Short: "Register a new MCP server under an app (pretty JSON)",
	Long: `Register a new HOSTED or EXTERNAL MCP server under an existing app.

HOSTED servers run inside C1 from a catalog impl — browse
"c1i mcp servers catalog list" and pass the entry's id via --catalog-id.
EXTERNAL servers point at a third-party MCP URL via --url.

Auth (convenience flags cover the simple methods):
  --auth none
  --auth bearer-token  --bearer-token TOKEN
  --auth custom-header --header-name NAME --header-value VALUE
  --auth basic-auth    --basic-auth-username USER --basic-auth-password PASS
For OAuth2 / AWS SigV4 / Google service-account auth, pass the full config
object via --hosted-config-file / --external-config-file (JSON, or "-" for
stdin). Auth secrets are sealed server-side; reads only return *_configured.

NOTE: registering under app_id="" (new managed app) or via an
app_managed_state_binding_ref is not reachable over REST — this command
requires --app-id. Approve discovered tools afterward with
"c1i mcp tools approve".`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id", "type", "display-name"); err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		appID, _ := cmd.Flags().GetString("app-id")
		body, err := buildRegisterBody(cmd)
		if err != nil {
			return err
		}

		path := client.Path("/api/v1/apps/%s/mcp_servers", appID)
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

// buildRegisterBody assembles the RegisterRequest body from flags. Pure (no
// network / auth), so the dry-run preview and unit tests exercise the same
// path the live request takes.
func buildRegisterBody(cmd *cobra.Command) (map[string]any, error) {
	appID, _ := cmd.Flags().GetString("app-id")
	typ, _ := cmd.Flags().GetString("type")
	displayName, _ := cmd.Flags().GetString("display-name")

	body := map[string]any{
		"appId":       appID,
		"serverType":  mapServerType(typ),
		"displayName": displayName,
	}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}
	if v, _ := cmd.Flags().GetString("data-sensitivity"); v != "" {
		body["dataSensitivity"] = mapDataSensitivity(v)
	}
	if v, _ := cmd.Flags().GetString("tool-prefix"); v != "" {
		body["toolPrefix"] = v
	}
	if ids, _ := cmd.Flags().GetStringSlice("user-id"); len(ids) > 0 {
		body["userIds"] = ids
	}

	switch strings.ToLower(typ) {
	case "hosted":
		cfg, err := buildHostedConfig(cmd)
		if err != nil {
			return nil, err
		}
		if !cmd.Flags().Changed("hosted-config-file") {
			if _, ok := cfg["mcpServerCatalogId"]; !ok {
				return nil, fmt.Errorf("--catalog-id is required for --type hosted (or pass --hosted-config-file)")
			}
		}
		body["hostedConfig"] = cfg
	case "external":
		cfg, err := buildExternalConfig(cmd)
		if err != nil {
			return nil, err
		}
		if !cmd.Flags().Changed("external-config-file") {
			if _, ok := cfg["url"]; !ok {
				return nil, fmt.Errorf("--url is required for --type external (or pass --external-config-file)")
			}
		}
		body["externalConfig"] = cfg
	default:
		return nil, fmt.Errorf("invalid --type %q: use hosted or external", typ)
	}

	return body, nil
}

func init() {
	mcpServersRegisterCmd.Flags().String("app-id", "", "Application ID to register under")
	mcpServersRegisterCmd.Flags().String("type", "", "Server type: hosted or external")
	mcpServersRegisterCmd.Flags().String("display-name", "", "Display name")
	mcpServersRegisterCmd.Flags().String("description", "", "Description")
	mcpServersRegisterCmd.Flags().String("data-sensitivity", "", "Data sensitivity: public, internal, confidential, restricted")
	mcpServersRegisterCmd.Flags().String("tool-prefix", "", "Prefix for exposed tool names")
	mcpServersRegisterCmd.Flags().StringSlice("user-id", nil, "Integration owner user ID (repeatable)")
	// HOSTED config
	mcpServersRegisterCmd.Flags().String("catalog-id", "", "Catalog entry ID (HOSTED)")
	mcpServersRegisterCmd.Flags().String("source-app-id", "", "Source app ID for connector-backed HOSTED servers")
	mcpServersRegisterCmd.Flags().StringSlice("config-field", nil, "Extra config field key=value (HOSTED, repeatable)")
	mcpServersRegisterCmd.Flags().String("hosted-config-file", "", "Full hostedConfig JSON (file or \"-\" for stdin)")
	// EXTERNAL config
	mcpServersRegisterCmd.Flags().String("url", "", "External MCP server URL (EXTERNAL)")
	mcpServersRegisterCmd.Flags().String("transport", "", "Transport: streamable-http or sse (EXTERNAL)")
	mcpServersRegisterCmd.Flags().String("external-config-file", "", "Full externalConfig JSON (file or \"-\" for stdin)")
	// Shared auth
	addAuthFlags(mcpServersRegisterCmd)
	markRequired(mcpServersRegisterCmd, "app-id", "type", "display-name")
	mcpServersCmd.AddCommand(mcpServersRegisterCmd)
}
