package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpServersCatalogGetCmd = &cobra.Command{
	Use:   "get <catalog-id>",
	Short: "Get a single MCP server catalog entry (pretty JSON)",
	Long: `Retrieve one catalog entry by its catalog_id, including the full
auth_modes and config_schema needed to build a register request.

Each entry in authModes carries its own "scopes" (required to register with
that auth method) and "optionalScopes" (grants extra tools/permissions but
isn't required) — that's where real scope tiering lives; it's per auth mode,
not a single catalog-entry-wide list. "catalog list" summarizes both into
required_scope_count/optional_scope_count. The top-level "defaultScopes"
field exists in the schema but is empty on every catalog entry observed in
production data.`,
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

		path := client.Path("/api/v1/mcp_server_catalog/%s", args[0])
		data, err := c.Get(cmd.Context(), path, nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeResource(cmd, data, "id")
	},
}

func init() {
	mcpServersCatalogCmd.AddCommand(mcpServersCatalogGetCmd)
}
