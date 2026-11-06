package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var webhooksCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a webhook",
	Long: `Create a webhook. --display-name and --destination-url are required.
--destination-url must be an absolute https:// URL; anything else is refused
with exit 2 before a request is sent, because the server would silently
rewrite it to https. No secret is returned. Honors --dry-run.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		body, _, err := webhookFields(cmd)
		if err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}
		if dryRunActive() {
			return printDryRun(cmd, "POST", webhooksPath, body)
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Post(cmd.Context(), webhooksPath, body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeRawObject(cmd, data)
	},
}

func init() {
	webhookFieldFlags(webhooksCreateCmd, "Webhook")
	markRequired(webhooksCreateCmd, "display-name", "destination-url")
	webhooksCmd.AddCommand(webhooksCreateCmd)
}
