package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var accessProfilesDeleteCmd = &cobra.Command{
	Use:   "delete <access-profile-id>",
	Short: "Soft-delete an access profile",
	Long: `Soft-delete an access profile by ID. Prints the API's response, which is {};
exit 0 is the success signal. Honors --dry-run.

The catalog is retained for audit: it leaves "access-profiles list", but a
direct "access-profiles get" still returns it with deletedAt populated.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: client.Path("/api/v1/catalogs/%s", args[0])})
	},
}

func init() {
	markMutating(accessProfilesDeleteCmd)
	accessProfilesCmd.AddCommand(accessProfilesDeleteCmd)
}
