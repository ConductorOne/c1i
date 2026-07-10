package cmd

import (
	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// newClient builds an authenticated API client for baseURL, applying the
// global configuration resolved by cobra/viper (currently the retry budget).
// All commands go through this helper so cross-cutting client options are
// wired in one place rather than at every call site.
func newClient(cmd *cobra.Command, baseURL string) (*client.Client, error) {
	return client.New(cmd.Context(), baseURL,
		client.WithMaxRetries(viper.GetInt("max_retries")),
		client.WithDebug(viper.GetBool("debug")),
	)
}
