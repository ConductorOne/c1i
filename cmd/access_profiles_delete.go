package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var accessProfilesDeleteCmd = &cobra.Command{
	Use:   "delete <access-profile-id>",
	Short: "Soft-delete an access profile (pretty JSON)",
	Long: `Soft-delete an access profile by ID. Honors --dry-run.

The catalog is retained for audit: it leaves "access-profiles list", but a
direct "access-profiles get" still returns it with deletedAt populated.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		path := client.Path("/api/v1/catalogs/%s", args[0])
		if dryRunActive() {
			return printDryRun(cmd, "DELETE", path, nil)
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Delete(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeRawObject(cmd, data)
	},
}

func init() {
	accessProfilesCmd.AddCommand(accessProfilesDeleteCmd)
}
