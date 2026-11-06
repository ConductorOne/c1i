package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var webhooksGetCmd = &cobra.Command{
	Use:   "get <webhook-id>",
	Short: "Get a single webhook by ID (pretty JSON)",
	Long: `Get a single webhook by ID (pretty JSON).

A deleted webhook is still returned at exit 0, with deletedAt set; "webhooks
list" omits it.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Get(cmd.Context(), webhookPath(args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeResource(cmd, data, "id")
	},
}

func init() {
	webhooksCmd.AddCommand(webhooksGetCmd)
}
