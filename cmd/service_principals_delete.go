package cmd

import (
	"net/http"

	"github.com/spf13/cobra"
)

var servicePrincipalsDeleteCmd = &cobra.Command{
	Use:   "delete <sp-id>",
	Short: "Delete a service principal and all its credentials",
	Long: `Delete a service principal and all its credentials.

Prints the API's response, which is {}; exit 0 is the success signal.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: spPath(args[0])})
	},
}

func init() {
	markMutating(servicePrincipalsDeleteCmd)
	servicePrincipalsCmd.AddCommand(servicePrincipalsDeleteCmd)
}
