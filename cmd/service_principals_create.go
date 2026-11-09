package cmd

import (
	"net/http"

	"github.com/spf13/cobra"
)

var servicePrincipalsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a service principal",
	Long: `Create a service principal.

Prints the service principal as pretty JSON, unwrapped like
"service-principals get": the id is .id.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		displayName, _ := cmd.Flags().GetString("display-name")
		body := map[string]any{"displayName": displayName}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: "/api/v1/service_principals", Body: body})
	},
}

func init() {
	servicePrincipalsCreateCmd.Flags().String("display-name", "", "Display name for the new service principal ("+displayNameRule+")")
	markRequired(servicePrincipalsCreateCmd, "display-name")
	markMutating(servicePrincipalsCreateCmd)
	servicePrincipalsCmd.AddCommand(servicePrincipalsCreateCmd)
}
