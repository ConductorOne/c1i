package cmd

import (
	"net/http"

	"github.com/spf13/cobra"
)

var appsResourceTypesDeleteCmd = &cobra.Command{
	Use:   "delete <resource-type-id>",
	Short: "Delete a manually managed resource type",
	Long: `Delete a manually managed resource type of an app. Honors --dry-run.

The delete is soft: "apps resource-types get" still returns the type with
deletedAt set, and "apps resource-types list" stops listing it. Repeating the
delete exits 0; an unknown app or resource type exits 4. Creating a resource
under a deleted type fails with a 500 (exit 6), "resource type is deleted".

` + resourceTypeCascadeNote + `

Prints the API's response, which is {}; exit 0 is the success signal.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: resourceTypePath(appID, args[0])})
	},
}

// resourceTypeCascadeNote is what a type delete does to its resources.
const resourceTypeCascadeNote = `The API spec says this deletes the type "and all its associated resources",
but a resource of a deleted type was still live (deletedAt null) ten minutes
later. Delete the resources first with "apps resources delete" if they must
go too.`

func init() {
	addResourceTypeAppFlag(appsResourceTypesDeleteCmd)
	markMutating(appsResourceTypesDeleteCmd)
	appsResourceTypesCmd.AddCommand(appsResourceTypesDeleteCmd)
}
