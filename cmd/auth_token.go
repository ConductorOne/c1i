package cmd

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var authTokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Print a short-lived OAuth2 bearer token for the C1 API",
	Long: `Mint and print a short-lived OAuth2 bearer token from the stored
credentials, for driving raw API calls yourself.

By default only the access token is printed (newline-terminated), so it
composes directly:

  curl -H "Authorization: Bearer $(c1i auth token)" \
    https://your-tenant.conductor.one/api/v1/...

Use --json to also see the token type and absolute expiry (RFC3339).

The token is audience-scoped to the C1 API host. It is not written to
disk; a new one is minted on each invocation.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		tok, err := client.Token(cmd.Context(), baseURL)
		if err != nil {
			// Only frame genuine auth failures (bad/missing credentials, a
			// rejected mint) as "not authenticated" → exit 3. A ctx
			// cancellation (Ctrl-C / timeout) is not an auth problem, so let it
			// pass through unwrapped (generic exit) rather than mislabel it.
			var authErr *client.AuthError
			if errors.As(err, &authErr) {
				return fmt.Errorf("not authenticated: %w", err)
			}
			return err
		}

		out := cmd.OutOrStdout()
		if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			return enc.Encode(map[string]string{
				"access_token": tok.AccessToken,
				"token_type":   tok.TokenType,
				"expires_at":   tok.Expiry.UTC().Format("2006-01-02T15:04:05Z07:00"),
			})
		}

		_, _ = fmt.Fprintln(out, tok.AccessToken)
		return nil
	},
}

func init() {
	authTokenCmd.Flags().Bool("json", false, "Emit access token, type, and absolute expiry as JSON")
	authCmd.AddCommand(authTokenCmd)
}
