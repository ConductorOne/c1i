package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var entitlementsDeleteCmd = &cobra.Command{
	Use:   "delete <entitlement-id>",
	Short: "Delete an app entitlement",
	Long: `Delete an app entitlement by ID. Entitlements are scoped to an app, so
--app-id is required in addition to the entitlement ID. Honors --dry-run.

The delete is soft: "entitlements get" still returns the entitlement at exit
0 with deletedAt set. "apps delete" leaves an app's entitlements live, so
delete them with this first.

Prints the API's response, which is {}; exit 0 is the success signal.

Example:
  c1i entitlements delete <entitlement-id> --app-id <app-id>`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: client.Path("/api/v1/apps/%s/entitlements/%s", appID, args[0])})
	},
}

func init() {
	entitlementsDeleteCmd.Flags().String("app-id", "", "Application ID the entitlement belongs to")
	markRequired(entitlementsDeleteCmd, "app-id")
	markMutating(entitlementsDeleteCmd)
	entitlementsCmd.AddCommand(entitlementsDeleteCmd)
}
