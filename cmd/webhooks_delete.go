package cmd

import (
	"net/http"

	"github.com/spf13/cobra"
)

var webhooksDeleteCmd = &cobra.Command{
	Use:   "delete <webhook-id>",
	Short: "Soft-delete a webhook",
	Long: `Soft-delete a webhook by ID. It leaves "webhooks list", but "webhooks get"
still returns it at exit 0 with deletedAt set, and deleting it again exits 0
again. Prints the API's response, which is {}; exit 0 is the success
signal. Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: webhookPath(args[0])})
	},
}

func init() {
	markMutating(webhooksDeleteCmd)
	webhooksCmd.AddCommand(webhooksDeleteCmd)
}
