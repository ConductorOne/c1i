package cmd

import (
	"net/http"

	"github.com/spf13/cobra"
)

var webhooksCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a webhook",
	Long: `Create a webhook. --display-name and --destination-url are required.

Refused with exit 2 before a request is sent:
  - a blank --display-name (empty, whitespace or invisible-only)
  - a --destination-url that is not https, has no host, or uses an IPv6
    literal host (the server would rewrite another scheme to https, and
    strips an IPv6 literal's brackets)
  - a --callback-timeout with a sign or decimal

The server also refuses a --display-name outside 3-100 characters with a 400
(exit 2).

No secret is returned. Prints the webhook, unwrapped like "webhooks get": the
id is .id. Honors --dry-run.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		body, _, err := webhookFields(cmd)
		if err != nil {
			return err
		}

		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: webhooksPath, Body: body})
	},
}

func init() {
	webhookFieldFlags(webhooksCreateCmd, false)
	markRequired(webhooksCreateCmd, "display-name", "destination-url")
	markMutating(webhooksCreateCmd)
	webhooksCmd.AddCommand(webhooksCreateCmd)
}
