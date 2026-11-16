package cmd

import (
	"errors"
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

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("not authenticated: %w", err)
		}

		// Probe Load directly to learn which backend served the credentials;
		// client.New does its own Load but doesn't expose the backend. It runs
		// after New, which may have moved an older key's credential to the
		// current one, so the key reported is the one that still exists.
		var service string
		var backend keychain.Backend
		loadErr := errors.New("no stored credentials")
		for _, k := range config.KeychainServices(baseURL) {
			if _, _, b, err := keychain.Load(k); err == nil {
				service, backend, loadErr = k, b, nil
				break
			}
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
