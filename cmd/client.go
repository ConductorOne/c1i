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
var newClient = func(cmd *cobra.Command, baseURL string) (*client.Client, error) {
	return client.New(cmd.Context(), baseURL,
		client.WithMaxRetries(viper.GetInt("max_retries")),
		client.WithDebug(viper.GetBool("debug")),
	)
}

// newListClient is the client constructor every auto-paginating list/search
// command calls, instead of newClient directly. It's a var, not a direct
// call, so a test can substitute an httptest-backed client across all of
// them from one seam — mirroring newAPIClient (cmd/api.go), newPoliciesClient
// (cmd/policies.go), and newGrantClient/newRevokeClient
// (cmd/requests_create_grant.go, cmd/requests_create_revoke.go).
var newListClient = newClient
