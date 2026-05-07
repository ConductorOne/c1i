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
		if err := keychain.Delete(service); err != nil {
			return fmt.Errorf("logout: %w", err)
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed credentials for %s.\n", baseURL)
		return nil
	},
}

func init() {
	authCmd.AddCommand(authLogoutCmd)
}
