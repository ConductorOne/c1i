package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var appsDeleteCmd = &cobra.Command{
	Use:   "delete <app-id>",
	Short: "Soft-delete an app",
	Long: `Soft-delete an app by ID: the app is marked deleted (deletedAt set)
rather than erased, which is why "apps list" rows carry a deleted_at field.
Honors --dry-run.

This complements "apps create" so a container app made by mistake (or a test
app) can be cleaned up without dropping to the raw "api" escape hatch.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		path := client.Path("/api/v1/apps/%s", args[0])
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

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Deleted app: id=%s\n", args[0])
		return nil
	},
}

func init() {
	appsCmd.AddCommand(appsDeleteCmd)
}
