package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/ConductorOne/c1i/internal/config"
	"github.com/ConductorOne/c1i/internal/keychain"
	"github.com/spf13/cobra"
)

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check whether valid C1 credentials are stored and working",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		// Probe Load directly to learn which backend served the credentials.
		// client.New does its own Load but doesn't expose the backend.
		service := config.KeychainService(baseURL)
		_, _, backend, loadErr := keychain.Load(service)

		c, err := client.New(cmd.Context(), baseURL)
		if err != nil {
			return fmt.Errorf("not authenticated: %w", err)
		}

		body := map[string]any{"pageSize": 1}
		if _, err := c.Post(cmd.Context(), "/api/v1/search/users", body); err != nil {
			return fmt.Errorf("credentials found but API test failed: %w", err)
		}

		out := cmd.OutOrStdout()
		_, _ = fmt.Fprintf(out, "Authenticated to %s.\n", baseURL)
		if loadErr == nil {
			switch backend {
			case keychain.BackendEnv:
				_, _ = fmt.Fprintln(out, "Source: environment variables (C1I_CLIENT_ID, C1I_CLIENT_SECRET)")
			case keychain.BackendKeyring:
				_, _ = fmt.Fprintf(out, "Source: %s\n", keyringName())
			case keychain.BackendFile:
				if path, perr := keychain.FilePath(service); perr == nil {
					_, _ = fmt.Fprintf(out, "Source: file %s\n", path)
				} else {
					_, _ = fmt.Fprintln(out, "Source: file fallback")
				}
			}
		}
		return nil
	},
}

func init() {
	authCmd.AddCommand(authStatusCmd)
}
