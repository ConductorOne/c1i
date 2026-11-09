package cmd

import (
	"github.com/ConductorOne/c1i/internal/client"
	"github.com/ConductorOne/c1i/internal/transport"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// newClient builds an authenticated API client for baseURL, applying the
// global configuration resolved by cobra/viper (currently the retry budget).
// All commands go through this helper so cross-cutting client options are
// wired in one place rather than at every call site.
var newClient = func(cmd *cobra.Command, baseURL string) (*client.Client, error) {
	return client.New(cmd.Context(), baseURL, globalClientOptions()...)
}

// globalClientOptions and globalTransportOptions are the only places the
// global --max-retries/--debug flags are turned into options. Every client the
// CLI builds takes one of them, so a new flag of this kind is wired once.
func globalClientOptions() []client.Option {
	return []client.Option{
		client.WithMaxRetries(viper.GetInt("max_retries")),
		client.WithDebug(debugWriter()),
	}
}

func globalTransportOptions() []transport.Option {
	return []transport.Option{
		transport.WithMaxRetries(viper.GetInt("max_retries")),
		transport.WithDebug(debugWriter()),
	}
}

// newListClient is the client constructor every auto-paginating list/search
// command calls, instead of newClient directly. It's a var, not a direct
// call, so a test can substitute an httptest-backed client across all of
// them from one seam — mirroring newAPIClient (cmd/api.go) and
// newPoliciesClient (cmd/policies.go).
var newListClient = newClient
