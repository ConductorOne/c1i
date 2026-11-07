package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var entitlementsDeleteCmd = &cobra.Command{
	Use:   "delete <entitlement-id>",
	Short: "Delete an app entitlement (pretty JSON)",
	Long: `Delete an app entitlement by ID. Entitlements are scoped to an app, so
--app-id is required in addition to the entitlement ID. Honors --dry-run.

The delete is soft: "entitlements get" still returns the entitlement at exit
0 with deletedAt set. "apps delete" leaves an app's entitlements live, so
delete them with this first.

Prints the server's response as pretty JSON (--fields is not applied to
mutation output).

Example:
  c1i entitlements delete <entitlement-id> --app-id <app-id>`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id"); err != nil {
			return err
		}
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		appID, _ := cmd.Flags().GetString("app-id")
		path := client.Path("/api/v1/apps/%s/entitlements/%s", appID, args[0])
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
	entitlementsDeleteCmd.Flags().String("app-id", "", "Application ID the entitlement belongs to")
	markRequired(entitlementsDeleteCmd, "app-id")
	entitlementsCmd.AddCommand(entitlementsDeleteCmd)
}
