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
auth_modes and config_schema needed to build a register request.`,
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

		return writeObject(cmd, data)
	},
}

func init() {
	mcpServersCatalogCmd.AddCommand(mcpServersCatalogGetCmd)
}
