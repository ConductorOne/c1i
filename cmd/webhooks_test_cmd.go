package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var webhooksTestCmd = &cobra.Command{
	Use:   "test <webhook-id>",
	Short: "Queue a test delivery to a webhook's URL",
	Long: `Queue a test delivery to a webhook's URL and print the delivery instance
(pretty JSON). Its .id is the delivery; .webhookId is the webhook you tested.

The delivery is asynchronous: the response is the queued instance with
"state": "WEBHOOK_STATE_PENDING" and exit 0 whether or not the URL is
reachable, so exit 0 does not mean the endpoint answered. Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := client.Path(webhooksPath+"/%s/test", args[0])
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: map[string]any{}})
	},
}

func init() {
	markMutating(webhooksTestCmd)
	webhooksCmd.AddCommand(webhooksTestCmd)
}
