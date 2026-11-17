package cmd

import (
	"net/http"

	"github.com/spf13/cobra"
)

var webhooksUpdateCmd = &cobra.Command{
	Use:   "update <webhook-id>",
	Short: "Update a webhook's display name, URL, description or callback timeout",
	Long: `Update a webhook. Only the flags you pass are sent, and each is named in the
update mask; an empty --description is sent as an empty value.

Refused with exit 2 before a request is sent:
  - no field flag at all
  - a blank --display-name (empty, whitespace or invisible-only; the server
    would store it on update)
  - a --destination-url that is not https, has no host, or uses an IPv6
    literal host
  - a --callback-timeout with a sign or decimal

The server also refuses a --display-name outside 3-100 characters with a 400
(exit 2).

Updating a soft-deleted webhook exits 0 and changes it; it stays deleted.
Prints the webhook, unwrapped like "webhooks get": the id is .id. Honors
--dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		fields := map[string]any{}
		mask, err := webhookFields.build(cmd, fields, nil)
		if err != nil {
			return err
		}
		fields["id"] = id

		body := wrapUpdate("webhook", fields, mask)
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: webhookPath(id), Body: body})
	},
}

func init() {
	webhookFieldFlags(webhooksUpdateCmd, true)
	registerUpdateSpec(webhooksUpdateCmd, webhookFields)
	markMutating(webhooksUpdateCmd)
	webhooksCmd.AddCommand(webhooksUpdateCmd)
}
