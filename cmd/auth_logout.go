package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/config"
	"github.com/ConductorOne/c1i/internal/keychain"
	"github.com/spf13/cobra"
)

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove stored C1 credentials for the current URL",
	Long: `Remove credentials stored for the current C1 URL from both the OS
keyring and the file fallback. Environment variables (C1I_CLIENT_ID,
C1I_CLIENT_SECRET) are not affected.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}
		service := config.KeychainService(baseURL)
		removed, err := keychain.Delete(service)
		if err != nil {
			return fmt.Errorf("logout: %w", err)
		}

		out := cmd.OutOrStdout()
		if removed {
			_, _ = fmt.Fprintf(out, "Removed credentials for %s.\n", baseURL)
		} else {
			_, _ = fmt.Fprintf(out, "No stored credentials to remove for %s.\n", baseURL)
		}
		if keychain.EnvCredentialsSet() {
			_, _ = fmt.Fprintln(out, "Note: C1I_CLIENT_ID/C1I_CLIENT_SECRET are still set in your environment and take precedence; unset them to fully log out.")
		}
		return nil
	},
}

func init() {
	authCmd.AddCommand(authLogoutCmd)
}
