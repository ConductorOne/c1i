package cmd

import (
	"net/http"

	"github.com/spf13/cobra"
)

var servicePrincipalsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a service principal",
	RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := requireNonEmptyIfSet(cmd, "display-name"); err != nil {
			return err
		}
		if err := requireNonEmpty(cmd, "display-name"); err != nil {
			return err
		}
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
