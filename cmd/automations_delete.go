package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var automationsDeleteCmd = &cobra.Command{
	Use:   "delete <automation-id>",
	Short: "Soft-delete an automation",
	Long: `Soft-delete an automation by ID. It leaves "automations list", but
"automations get" still returns it at exit 0, with no field marking it
deleted, and deleting it again exits 0 again. Prints the API's response, which is {};
exit 0 is the success signal. Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: client.Path("/api/v1/automations/%s", args[0])})
	},
}

func init() {
	markMutating(automationsDeleteCmd)
	automationsCmd.AddCommand(automationsDeleteCmd)
}
