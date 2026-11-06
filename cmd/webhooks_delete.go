package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var webhooksDeleteCmd = &cobra.Command{
	Use:   "delete <webhook-id>",
	Short: "Soft-delete a webhook",
	Long: `Soft-delete a webhook by ID. It leaves "webhooks list", but "webhooks get"
still returns it at exit 0 with deletedAt set, and deleting it again exits 0
again. Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		path := webhookPath(args[0])
		if dryRunActive() {
			return printDryRun(cmd, "DELETE", path, nil)
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		if _, err := c.Delete(cmd.Context(), path); err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Deleted webhook: id=%s\n", args[0]); err != nil {
			return wrapOutputErr(err)
		}
		return nil
	},
}

func init() {
	webhooksCmd.AddCommand(webhooksDeleteCmd)
}
