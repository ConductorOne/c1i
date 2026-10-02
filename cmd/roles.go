package cmd

import (
	"fmt"
	"strconv"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var rolesCmd = &cobra.Command{
	Use:   "roles",
	Short: "List and inspect IAM roles (the source of --scoped-role IDs)",
}

// roleListItem is the subset of Role the login menu needs.
type roleListItem struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	DisplayName   string `json:"displayName"`
	SystemAPIOnly bool   `json:"systemApiOnly"`
}

func roleRow(item map[string]any) map[string]any {
	return map[string]any{
		"id":              item["id"],
		"name":            item["name"],
		"display_name":    item["displayName"],
		"system_builtin":  item["systemBuiltin"] == true,
		"system_api_only": item["systemApiOnly"] == true,
		"created_at":      item["createdAt"],
		"updated_at":      item["updatedAt"],
	}
}

var rolesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List IAM roles (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return listJSONPages(cmd, roleRow, func(c *client.Client, size int, token string) ([]byte, error) {
			params := map[string]string{"page_size": strconv.Itoa(size)}
			if token != "" {
				params["page_token"] = token
			}
			return c.Get(cmd.Context(), "/api/v1/iam/roles", params)
		})
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
