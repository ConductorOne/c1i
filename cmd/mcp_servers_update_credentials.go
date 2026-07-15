package cmd

import (
	"fmt"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpServersUpdateCredentialsCmd = &cobra.Command{
	Use:   "update-credentials",
	Short: "Replace an MCP server's auth config / config fields (pretty JSON)",
	Long: `Replace the auth config and/or extra config fields on an existing MCP server.
Reuses the same sealing + validation path as register, so auth secrets are
re-sealed server-side.

Pick the config shape with --type (must match the server): hosted or external.
Supply the new config with the same convenience auth flags as register, or the
full --hosted-config-file / --external-config-file JSON.

By default the update_mask replaces the whole config object. Pass --update-mask
with comma-separated proto field paths (e.g. "hostedConfig.bearerToken") to
scope the change to specific subfields.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id", "connector-id", "type"); err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		appID, _ := cmd.Flags().GetString("app-id")
		connectorID, _ := cmd.Flags().GetString("connector-id")
		typ, _ := cmd.Flags().GetString("type")

		body := map[string]any{
			"appId":       appID,
			"connectorId": connectorID,
		}

		var cfg map[string]any
		var defaultMask string
		switch strings.ToLower(typ) {
		case "hosted":
			cfg, err = buildHostedConfig(cmd)
			defaultMask = "hostedConfig"
		case "external":
			cfg, err = buildExternalConfig(cmd)
			defaultMask = "externalConfig"
		default:
			return fmt.Errorf("invalid --type %q: use hosted or external", typ)
		}
		if err != nil {
			return err
		}

		mask, _ := cmd.Flags().GetString("update-mask")
		// Guard the destructive case: an empty config with the default
		// whole-config mask would replace the server's entire auth/config with
		// nothing (wiping catalog id, URL, sealed secrets). Require the caller
		// to supply what to set. An explicit --update-mask is trusted as intent.
		if len(cfg) == 0 && mask == "" {
			return fmt.Errorf("nothing to update: supply the new config via the auth flags (--auth/--bearer-token/…), --config-field, --url, or --%s-config-file", strings.ToLower(typ))
		}
		body[defaultMask] = cfg

		if mask != "" {
			body["updateMask"] = mask
		} else {
			body["updateMask"] = defaultMask
		}

		path := client.Path("/api/v1/apps/%s/mcp_servers/%s/credentials", appID, connectorID)
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
	mcpServersUpdateCredentialsCmd.Flags().String("app-id", "", "Application ID")
	mcpServersUpdateCredentialsCmd.Flags().String("connector-id", "", "MCP server (connector) ID")
	mcpServersUpdateCredentialsCmd.Flags().String("type", "", "Config shape (must match the server): hosted or external")
	// HOSTED config
	mcpServersUpdateCredentialsCmd.Flags().String("catalog-id", "", "Catalog entry ID (HOSTED)")
	mcpServersUpdateCredentialsCmd.Flags().String("source-app-id", "", "Source app ID (HOSTED)")
	mcpServersUpdateCredentialsCmd.Flags().StringSlice("config-field", nil, "Extra config field key=value (HOSTED, repeatable)")
	mcpServersUpdateCredentialsCmd.Flags().String("hosted-config-file", "", "Full hostedConfig JSON (file or \"-\" for stdin)")
	// EXTERNAL config
	mcpServersUpdateCredentialsCmd.Flags().String("url", "", "External MCP server URL (EXTERNAL)")
	mcpServersUpdateCredentialsCmd.Flags().String("transport", "", "Transport: streamable-http or sse (EXTERNAL)")
	mcpServersUpdateCredentialsCmd.Flags().String("external-config-file", "", "Full externalConfig JSON (file or \"-\" for stdin)")
	// Shared auth
	addAuthFlags(mcpServersUpdateCredentialsCmd)
	mcpServersUpdateCredentialsCmd.Flags().String("update-mask", "", "Comma-separated proto field paths to update (default: whole config)")
	markRequired(mcpServersUpdateCredentialsCmd, "app-id", "connector-id", "type")
	mcpServersCmd.AddCommand(mcpServersUpdateCredentialsCmd)
}
