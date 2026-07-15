package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var mcpServersDiscoverOIDCCmd = &cobra.Command{
	Use:   "discover-oidc",
	Short: "Fetch an issuer's OIDC discovery document (pretty JSON)",
	Long: `Fetch the OpenID Connect discovery document for an issuer and return its
authorization endpoint, token endpoint, supported scopes, and PKCE methods.
Use it to fill in the OAuth2 fields of a register / update-credentials request.

The issuer URL is the base (e.g. "https://accounts.google.com"); C1 appends
/.well-known/openid-configuration itself.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "issuer-url"); err != nil {
			return err
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		issuerURL, _ := cmd.Flags().GetString("issuer-url")
		body := map[string]any{"issuerUrl": issuerURL}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Post(cmd.Context(), "/api/v1/mcp_servers/discover_oidc", body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		return writeObject(cmd, data)
	},
}

func init() {
	mcpServersDiscoverOIDCCmd.Flags().String("issuer-url", "", "OIDC issuer base URL")
	markRequired(mcpServersDiscoverOIDCCmd, "issuer-url")
	mcpServersCmd.AddCommand(mcpServersDiscoverOIDCCmd)
}
