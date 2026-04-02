package cmd

import (
	"fmt"

	"github.com/ductone/c1i/internal/client"
	"github.com/spf13/cobra"
)

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check whether valid ConductorOne credentials are stored and working",
	RunE: func(cmd *cobra.Command, args []string) error {
		tenant, err := GetTenant()
		if err != nil {
			return err
		}

		c, err := client.New(cmd.Context(), tenant)
		if err != nil {
			return fmt.Errorf("not authenticated: %w", err)
		}

		body := map[string]any{"pageSize": 1}
		if _, err := c.Post(cmd.Context(), "/api/v1/search/users", body); err != nil {
			return fmt.Errorf("credentials found but API test failed: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Authenticated to tenant %q.\n", tenant)
		return nil
	},
}

func init() {
	authCmd.AddCommand(authStatusCmd)
}
