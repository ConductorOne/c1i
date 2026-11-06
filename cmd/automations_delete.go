package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var automationsDeleteCmd = &cobra.Command{
	Use:   "delete <automation-id>",
	Short: "Soft-delete an automation",
	Long: `Soft-delete an automation by ID. It leaves "automations list", but
"automations get" still returns it at exit 0, with no field marking it
deleted, and deleting it again exits 0 again. Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		path := client.Path("/api/v1/automations/%s", args[0])
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

		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Deleted automation: id=%s\n", args[0]); err != nil {
			return wrapOutputErr(err)
		}
		return nil
	},
}

func init() {
	automationsCmd.AddCommand(automationsDeleteCmd)
}
