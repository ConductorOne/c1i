package cmd

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

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
		var paths []string
		if cmd.Flags().Changed("display-name") {
			v, _ := cmd.Flags().GetString("display-name")
			sp["displayName"] = v
			paths = append(paths, "displayName")
		}
		if len(paths) == 0 {
			return &usageError{fmt.Errorf("nothing to update: pass --display-name")}
		}

		body := map[string]any{
			"servicePrincipal": sp,
			"updateMask":       strings.Join(paths, ","),
		}
		return sendMutation(cmd, mutation{Method: http.MethodPatch, Path: spPath(id), Body: body})
	},
}

func init() {
	servicePrincipalsUpdateCmd.Flags().String("display-name", "", "New display name (omit to leave unchanged; "+displayNameRule+")")
	markMutating(servicePrincipalsUpdateCmd)
	servicePrincipalsCmd.AddCommand(servicePrincipalsUpdateCmd)
}
