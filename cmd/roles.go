package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var rolesCmd = &cobra.Command{
	Use:   "roles",
	Short: "List and inspect IAM roles (find role IDs for --scoped-role)",
}

func roleRow(item map[string]any) map[string]any {
	return map[string]any{
		"id":              optString(item, "id"),
		"name":            optString(item, "name"),
		"display_name":    optString(item, "displayName"),
		"system_builtin":  item["systemBuiltin"] == true,
		"system_api_only": item["systemApiOnly"] == true,
		"created_at":      optString(item, "createdAt"),
		"updated_at":      optString(item, "updatedAt"),
	}
}

var rolesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List IAM roles (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return listMapPages(cmd, "GET /api/v1/iam/roles", "list", []string{"id"}, roleRow, getPage(cmd.Context(), "/api/v1/iam/roles", nil))
	},
}

var rolesGetCmd = &cobra.Command{
	Use:   "get <role-id>",
	Short: "Get a single IAM role, including its permissions (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		data, err := c.Get(cmd.Context(), client.Path("/api/v1/iam/roles/%s", args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeResource(cmd, data, "id")
	},
}

func init() {
	addPaginationFlags(rolesListCmd)
	rolesCmd.AddCommand(rolesListCmd, rolesGetCmd)
	rootCmd.AddCommand(rolesCmd)
}
