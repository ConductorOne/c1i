package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var webhooksTestCmd = &cobra.Command{
	Use:   "test <webhook-id>",
	Short: "Queue a test delivery to a webhook's URL",
	Long: `Queue a test delivery to a webhook's URL and print the delivery instance
(pretty JSON, under "webhook").

The delivery is asynchronous: the response is the queued instance with
"state": "WEBHOOK_STATE_PENDING" and exit 0 whether or not the URL is
reachable, so exit 0 does not mean the endpoint answered. Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		path := client.Path(webhooksPath+"/%s/test", args[0])
		body := map[string]any{}
		if dryRunActive() {
			return printDryRun(cmd, "POST", path, body)
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Post(cmd.Context(), path, body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeRawObject(cmd, data)
	},
}

func init() {
	webhooksCmd.AddCommand(webhooksTestCmd)
}
