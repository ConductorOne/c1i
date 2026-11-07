package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var appsDeleteCmd = &cobra.Command{
	Use:   "delete <app-id>",
	Short: "Soft-delete an app",
	Long: `Soft-delete an app by ID: the app is marked deleted (deletedAt set)
rather than erased, which is why "apps list" rows carry a deleted_at field.
Prints the API's response, which is {}; exit 0 is the success signal.
Honors --dry-run.

This complements "apps create" so a container app made by mistake (or a test
app) can be cleaned up without dropping to the raw "api" escape hatch.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: client.Path("/api/v1/apps/%s", args[0])})
	},
}

func init() {
	markMutating(appsDeleteCmd)
	appsCmd.AddCommand(appsDeleteCmd)
}
