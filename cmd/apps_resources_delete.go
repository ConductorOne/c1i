package cmd

import (
	"net/http"

	"github.com/spf13/cobra"
)

var appsResourcesDeleteCmd = &cobra.Command{
	Use:   "delete <resource-id>",
	Short: "Delete a manually managed resource",
	Long: `Delete a manually managed resource. Honors --dry-run.

The delete is soft: "apps resources get" still returns the resource with
deletedAt set, while "apps resources list" and "search" stop returning it
(they can trail the delete by a few seconds). Repeating the delete exits 0;
an unknown app, resource type or resource exits 4.

Prints the API's response, which is {}; exit 0 is the success signal.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: resourceScopeOf(cmd).resourcePath(args[0])})
	},
}

func init() {
	addResourceScopeFlags(appsResourcesDeleteCmd)
	markMutating(appsResourcesDeleteCmd)
	appsResourcesCmd.AddCommand(appsResourcesDeleteCmd)
}
