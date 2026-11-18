package cmd

import (
	"encoding/json"
	"fmt"
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

The server resolves the credential id globally and ignores the app and
connector path segments, so c1i refuses a credential whose appId or
connectorId differs from --app-id/--connector-id (exit 4).

A revoked credential is still returned with exit 0: in the run checked, revoke
set .expiresTime to a time in the past and left .deletedAt null.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return getConnectorResource(cmd, connectorCredentialPath(cmd, args[0]), func(data []byte) error {
			if err := requireCredentialScope(cmd, data); err != nil {
				return err
			}
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

// requireCredentialScope refuses a credential the server returned from a
// different app or connector. The read resolves the credential id globally and
// ignores both path segments, so the flags would otherwise look enforced.
func requireCredentialScope(cmd *cobra.Command, data []byte) error {
	var resp struct {
		Credential *struct {
			ID          string `json:"id"`
			AppID       string `json:"appId"`
			ConnectorID string `json:"connectorId"`
		} `json:"credential"`
	}
	if json.Unmarshal(data, &resp) != nil || resp.Credential == nil {
		return nil // writeResource reports a shape it cannot print
	}
	appID, _ := cmd.Flags().GetString("app-id")
	connectorID, _ := cmd.Flags().GetString("connector-id")
	c := resp.Credential
	if (c.AppID != "" && c.AppID != appID) || (c.ConnectorID != "" && c.ConnectorID != connectorID) {
		return &refNotFoundError{msg: fmt.Sprintf(
			"credential %q belongs to app %q and connector %q, not --app-id %q --connector-id %q",
			c.ID, c.AppID, c.ConnectorID, appID, connectorID)}
	}
	return nil
}
