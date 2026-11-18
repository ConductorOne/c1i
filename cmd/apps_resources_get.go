package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var appsResourcesGetCmd = &cobra.Command{
	Use:   "get <resource-id>",
	Short: "Get one resource of an app (pretty JSON)",
	Long: `Get one resource of an app (pretty JSON), unwrapped: the resource's own keys
are at the top level and its id is .id; the view's other keys
(objectPermissions, the *Path keys) stay alongside. A deleted resource is
still returned, with deletedAt set.

An unknown app, resource type or resource exits 4.`,
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
		data, err := c.Get(cmd.Context(), resourceScopeOf(cmd).resourcePath(args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeResource(cmd, data, "id")
	},
}

func init() {
	addResourceScopeFlags(appsResourcesGetCmd)
	appsResourcesCmd.AddCommand(appsResourcesGetCmd)
}
