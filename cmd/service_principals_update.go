package cmd

import (
	"net/http"

	"github.com/spf13/cobra"
)

var servicePrincipalsUpdateSpec = &updateSpec{fields: []updateField{strUpdate("display-name", "displayName")}}

var servicePrincipalsUpdateCmd = &cobra.Command{
	Use:   "update <sp-id>",
	Short: "Update a service principal's display name",
	Long: `Update a service principal's display name.

A blank --display-name (empty, whitespace or invisible-only) exits 2,
nothing sent. Sent as is, the server stores "" as an empty name and keeps
"  " verbatim.

Prints the service principal as pretty JSON, unwrapped like
"service-principals get": the id is .id.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		sp := map[string]any{"id": id}
		mask, err := servicePrincipalsUpdateSpec.build(cmd, sp)
		if err != nil {
			return err
		}
		return sendMutation(cmd, mutation{Method: http.MethodPatch, Path: spPath(id), Body: wrapUpdate("servicePrincipal", sp, mask)})
	},
}

func init() {
	servicePrincipalsUpdateCmd.Flags().String("display-name", "", "New display name (omit to leave unchanged; "+displayNameRule+")")
	registerUpdateSpec(servicePrincipalsUpdateCmd, servicePrincipalsUpdateSpec)
	markMutating(servicePrincipalsUpdateCmd)
	servicePrincipalsCmd.AddCommand(servicePrincipalsUpdateCmd)
}
