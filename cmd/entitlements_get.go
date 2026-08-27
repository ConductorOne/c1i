package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var entitlementsGetCmd = &cobra.Command{
	Use:   "get <entitlement-id>",
	Short: "Get a single app entitlement by ID (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	Long: `Get a single app entitlement by ID.

Entitlements are scoped to an app, so --app-id is required in addition to the
entitlement ID.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id"); err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		appID, _ := cmd.Flags().GetString("app-id")

		data, err := c.Get(cmd.Context(), client.Path("/api/v1/apps/%s/entitlements/%s", appID, args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeResource(cmd, data, "id")
	},
}

func init() {
	entitlementsGetCmd.Flags().String("app-id", "", "Application ID the entitlement belongs to")
	markRequired(entitlementsGetCmd, "app-id")
	entitlementsCmd.AddCommand(entitlementsGetCmd)
}
