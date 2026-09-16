package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var servicePrincipalsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a service principal",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "display-name"); err != nil {
			return err
		}
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		displayName, _ := cmd.Flags().GetString("display-name")
		body := map[string]any{"displayName": displayName}

		if dryRunActive() {
			return printDryRun(cmd, "POST", "/api/v1/service_principals", body)
		}

		c, err := newServicePrincipalsClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Post(cmd.Context(), "/api/v1/service_principals", body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeRawObject(cmd, data)
	},
}

func init() {
	servicePrincipalsCreateCmd.Flags().String("display-name", "", "Display name for the new service principal (required)")
	markRequired(servicePrincipalsCreateCmd, "display-name")
	servicePrincipalsCmd.AddCommand(servicePrincipalsCreateCmd)
}
