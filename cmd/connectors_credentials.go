package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var connectorCredentialsCmd = &cobra.Command{
	Use:   "credentials",
	Short: "Manage the client credentials a connector uses to reach C1.ai",
	Long: `Manage the client credentials a connector uses to authenticate to C1.ai.

The API has no list of a connector's credentials: a credential id comes from
the output of "connectors credentials rotate".`,
}

func connectorCredentialPath(cmd *cobra.Command, credentialID string) string {
	appID, _ := cmd.Flags().GetString("app-id")
	connectorID, _ := cmd.Flags().GetString("connector-id")
	return client.Path("/api/v1/apps/%s/connectors/%s/credentials/%s", appID, connectorID, credentialID)
}

var connectorCredentialsGetCmd = &cobra.Command{
	Use:   "get <credential-id>",
	Short: "Get a connector credential (pretty JSON)",
	Long: `Get a connector credential. Output is the credential itself, not the API's
"credential" wrapper. .clientSecret is "" here; it is returned only by
"connectors credentials rotate".

A revoked credential is still returned with exit 0: in the run checked, revoke
set .expiresTime to a time in the past and left .deletedAt null.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return getConnectorResource(cmd, connectorCredentialPath(cmd, args[0]), func(data []byte) error {
			return writeResource(cmd, data, "id")
		})
	},
}

var connectorCredentialsRevokeCmd = &cobra.Command{
	Use:   "revoke <credential-id>",
	Short: "Revoke a connector credential",
	Long: `Revoke a connector credential. In the run checked, the credential stayed
readable with .expiresTime set to a time in the past, and revoking it again
exited 0.

Prints the API's response, which is {}. Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: connectorCredentialPath(cmd, args[0]), Body: map[string]any{}})
	},
}

var connectorCredentialsRotateCmd = &cobra.Command{
	Use:   "rotate",
	Short: "Issue a new connector credential (the secret is shown once)",
	Long: `Issue a new client credential for a connector. The secret is shown once.

Prints the new credential as pretty JSON, unwrapped like "connectors
credentials get": the id is .id, beside .clientId and .clientSecret. Capture
.clientSecret now; it cannot be read again. In the run checked, the previous
credential stayed valid with .expiresTime set seven days ahead; revoke it with
"connectors credentials revoke" to end it sooner.

A self-hosted connector authenticates with this credential: it keeps using
the old one until it is given the new one. Honors --dry-run.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		connectorID, _ := cmd.Flags().GetString("connector-id")
		body := map[string]any{"appId": appID, "connectorId": connectorID}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: "/api/v1/apps/connectors/credentials", Body: body},
			secretBearing(secretConnectorCredential))
	},
}

func init() {
	connectorsCmd.AddCommand(connectorCredentialsCmd)
	for _, c := range []*cobra.Command{connectorCredentialsGetCmd, connectorCredentialsRevokeCmd, connectorCredentialsRotateCmd} {
		c.Flags().String("app-id", "", "Application ID")
		c.Flags().String("connector-id", "", "Connector ID")
		markRequired(c, "app-id", "connector-id")
		connectorCredentialsCmd.AddCommand(c)
	}
	markMutating(connectorCredentialsRevokeCmd)
	markMutating(connectorCredentialsRotateCmd)
}
