package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/ConductorOne/c1i/internal/config"
	"github.com/ConductorOne/c1i/internal/keychain"
	"github.com/spf13/cobra"
)

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove stored C1 credentials for the current URL",
	Long: `Remove credentials stored for the current C1 URL from both the OS
keyring and the file fallback, including older copies of the same tenant's
credentials (a :443 name, the legacy *.conductor.one short name). Environment variables (C1I_CLIENT_ID,
C1I_CLIENT_SECRET) are not affected.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}
		warnPartialEnvCredentials(cmd)
		client.ClearCachedToken(baseURL)
		// Every key the credentials may live under, or a fallback would log
		// the next command straight back in. One line, whichever were removed.
		removed := false
		var firstErr error
		for _, service := range config.KeychainServices(baseURL) {
			r, err := keychain.Delete(service)
			if err != nil && firstErr == nil {
				firstErr = err
			}
			removed = removed || r
		}
		if firstErr != nil {
			return fmt.Errorf("logout: %w", firstErr)
		}

		out := cmd.OutOrStdout()
		if removed {
			if _, err := fmt.Fprintf(out, "Removed credentials for %s.\n", baseURL); err != nil {
				return wrapOutputErr(err)
			}
		} else {
			if _, err := fmt.Fprintf(out, "No stored credentials to remove for %s.\n", baseURL); err != nil {
				return wrapOutputErr(err)
			}
		}
		if keychain.EnvCredentialsSet() {
			if _, err := fmt.Fprintln(out, "Note: C1I_CLIENT_ID/C1I_CLIENT_SECRET are still set in your environment and take precedence; unset them to fully log out."); err != nil {
				return wrapOutputErr(err)
			}
		}
		return nil
	},
}

func init() {
	authCmd.AddCommand(authLogoutCmd)
}
