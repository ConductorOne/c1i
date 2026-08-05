package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var mcpServersCatalogGetCmd = &cobra.Command{
	Use:   "get",
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
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "catalog-id"); err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		catalogID, _ := cmd.Flags().GetString("catalog-id")

		path := client.Path("/api/v1/mcp_server_catalog/%s", catalogID)
		data, err := c.Get(cmd.Context(), path, nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeObject(cmd, data)
	},
}

func init() {
	mcpServersCatalogGetCmd.Flags().String("catalog-id", "", "Catalog entry ID")
	markRequired(mcpServersCatalogGetCmd, "catalog-id")
	mcpServersCatalogCmd.AddCommand(mcpServersCatalogGetCmd)
}
