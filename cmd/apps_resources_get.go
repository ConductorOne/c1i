package cmd

import (
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
		return getResource(cmd, resourceScopeOf(cmd).resourcePath(args[0]), nil, "id", "appResourceView", "appResource")
	},
}

func init() {
	addResourceScopeFlags(appsResourcesGetCmd)
	appsResourcesCmd.AddCommand(appsResourcesGetCmd)
}
