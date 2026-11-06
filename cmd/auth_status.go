package cmd

import (
	"fmt"

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

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("not authenticated: %w", err)
		}

		body := map[string]any{"pageSize": 1}
		if _, err := c.Post(cmd.Context(), "/api/v1/search/users", body); err != nil {
			return fmt.Errorf("credentials found but API test failed: %w", err)
		}

		out := cmd.OutOrStdout()
		if _, err := fmt.Fprintf(out, "Authenticated to %s.\n", baseURL); err != nil {
			return wrapOutputErr(err)
		}
		if loadErr == nil {
			switch backend {
			case keychain.BackendEnv:
				if _, err := fmt.Fprintln(out, "Source: environment variables (C1I_CLIENT_ID, C1I_CLIENT_SECRET)"); err != nil {
					return wrapOutputErr(err)
				}
			case keychain.BackendKeyring:
				if _, err := fmt.Fprintf(out, "Source: %s\n", keyringName()); err != nil {
					return wrapOutputErr(err)
				}
			case keychain.BackendFile:
				if path, perr := keychain.FilePath(service); perr == nil {
					if _, err := fmt.Fprintf(out, "Source: file %s\n", path); err != nil {
						return wrapOutputErr(err)
					}
				} else {
					if _, err := fmt.Fprintln(out, "Source: file fallback"); err != nil {
						return wrapOutputErr(err)
					}
				}
			}
		}
		return nil
	},
}

func init() {
	authCmd.AddCommand(authStatusCmd)
}
