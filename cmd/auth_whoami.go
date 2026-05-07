package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var authWhoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show the authenticated principal's user ID, roles, and permissions",
	Long: `Calls /api/v1/auth/introspect and prints the authenticated principal's
identity, roles, permissions, and tenant feature flags. Useful for agents
that need to discover the current user before making other API calls.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}
		c, err := client.New(cmd.Context(), baseURL)
		if err != nil {
			return fmt.Errorf("not authenticated: %w", err)
		}
		body, err := c.Get(cmd.Context(), "/api/v1/auth/introspect", nil)
		if err != nil {
			return err
		}
		var pretty any
		if err := json.Unmarshal(body, &pretty); err != nil {
			return fmt.Errorf("parsing introspect response: %w", err)
		}
		out, err := json.MarshalIndent(pretty, "", "  ")
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	},
}

func init() {
	authCmd.AddCommand(authWhoamiCmd)
}
