package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var mcpServersTestConnectionCmd = &cobra.Command{
	Use:   "test-connection [<connector-id>]",
	Short: "Probe an EXTERNAL MCP server's reachability (pretty JSON)",
	Long: `Probe whether an EXTERNAL MCP server is reachable with the supplied
credentials. Returns reachable (did MCP initialize + tools/list both succeed),
toolCount (string), and a sanitized failureReason.

Two modes:
  create — supply the config to probe: --url [--transport] [auth flags], or
           --external-config-file for full-fidelity config.
  edit   — probe an existing external server: <connector-id> + --app-id, with
           any changed fields named by --update-mask (omit for a stored-config
           probe).

EXTERNAL servers only; HOSTED servers return an error ("test connection is only
supported for external MCP servers").`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		appID, _ := cmd.Flags().GetString("app-id")
		var connectorID string
		if len(args) > 0 {
			connectorID = args[0]
		}
		editMode := appID != "" || connectorID != ""

		body := map[string]any{}
		if editMode {
			if connectorID == "" {
				return fmt.Errorf("edit mode requires <connector-id> as a positional argument (in addition to --app-id)")
			}
			if err := requireNonEmpty(cmd, "app-id"); err != nil {
				return err
			}
			body["appId"] = appID
			body["connectorId"] = connectorID
			if mask, _ := cmd.Flags().GetString("update-mask"); mask != "" {
				body["updateMask"] = mask
			}
		}

		// External config is required in create mode; in edit mode it carries
		// only the changed fields (named by --update-mask) and may be omitted.
		hasConfigInput := cmd.Flags().Changed("url") || cmd.Flags().Changed("transport") ||
			cmd.Flags().Changed("auth") || cmd.Flags().Changed("external-config-file")
		if hasConfigInput || !editMode {
			ext, err := buildExternalConfig(cmd)
			if err != nil {
				return err
			}
			if len(ext) > 0 {
				body["externalConfig"] = ext
			} else if !editMode {
				return fmt.Errorf("provide the config to probe: --url (with optional --transport/--auth) or --external-config-file, or pass <connector-id> and --app-id to probe an existing server")
			}
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Post(cmd.Context(), "/api/v1/mcp_servers/test_connection", body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeObject(cmd, data)
	},
}

func init() {
	mcpServersTestConnectionCmd.Flags().String("app-id", "", "Application ID of an existing external server (edit mode)")
	mcpServersTestConnectionCmd.Flags().String("url", "", "External MCP server URL (create mode)")
	mcpServersTestConnectionCmd.Flags().String("transport", "", "Transport: streamable-http or sse")
	mcpServersTestConnectionCmd.Flags().String("external-config-file", "", "Full externalConfig JSON (file or \"-\" for stdin)")
	mcpServersTestConnectionCmd.Flags().String("update-mask", "", "Comma-separated changed externalConfig paths (edit mode)")
	addAuthFlags(mcpServersTestConnectionCmd)
	mcpServersCmd.AddCommand(mcpServersTestConnectionCmd)
}
