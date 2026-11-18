package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// bridges manages tunnel bridges (c1.api.iam.v1.TunnelCredentialsService):
// the bridge record, the client credentials its appliance authenticates with,
// and the services the appliance announces.

const bridgesPath = "/api/v1/iam/tunnel/bridges"

var bridgesCmd = &cobra.Command{
	Use:     "bridges",
	Aliases: []string{"bridge"},
	Short:   "Manage tunnel bridges, their credentials, and announced services",
	Long: `Manage tunnel bridges. A bridge's appliance authenticates with a client
credential created here.

  bridges create --display-name "datacenter-1"
  bridges credentials create <bridge-id>
  bridges services <bridge-id>`,
}

func bridgePath(id string) string { return client.Path(bridgesPath+"/%s", id) }

func bridgeCredentialsPath(bridgeID string) string {
	return client.Path(bridgesPath+"/%s/credentials", bridgeID)
}

// bridge is the subset of TunnelBridge surfaced in list rows.
type bridge struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
	Appliance   *struct {
		Status                string `json:"status"`
		LastSeenAt            string `json:"lastSeenAt"`
		AnnouncedServiceCount int    `json:"announcedServiceCount"`
	} `json:"appliance"`
}

func bridgeRow(b bridge) map[string]any {
	row := map[string]any{
		"id":                      nilIfEmpty(b.ID),
		"display_name":            nilIfEmpty(b.DisplayName),
		"description":             nilIfEmpty(b.Description),
		"appliance_status":        nil,
		"last_seen_at":            nil,
		"announced_service_count": nil,
		"created_at":              nilIfEmpty(b.CreatedAt),
		"updated_at":              nilIfEmpty(b.UpdatedAt),
	}
	if a := b.Appliance; a != nil {
		row["appliance_status"] = nilIfEmpty(a.Status)
		row["last_seen_at"] = nilIfEmpty(a.LastSeenAt)
		row["announced_service_count"] = a.AnnouncedServiceCount
	}
	return row
}

// bridgeCredential is the subset of TunnelCredential surfaced in list rows.
type bridgeCredential struct {
	ID               string `json:"id"`
	BridgeID         string `json:"bridgeId"`
	ClientID         string `json:"clientId"`
	CredentialStatus string `json:"credentialStatus"`
	CreatedAt        string `json:"createdAt"`
	ExpiresTime      string `json:"expiresTime"`
	LastUsedAt       string `json:"lastUsedAt"`
	RevokedAt        string `json:"revokedAt"`
}

// bridgeCredentialRow never carries the secret: create returns it once.
func bridgeCredentialRow(c bridgeCredential) map[string]any {
	return map[string]any{
		"id":                nilIfEmpty(c.ID),
		"bridge_id":         nilIfEmpty(c.BridgeID),
		"client_id":         nilIfEmpty(c.ClientID),
		"credential_status": nilIfEmpty(c.CredentialStatus),
		"created_at":        nilIfEmpty(c.CreatedAt),
		"expires_time":      nilIfEmpty(c.ExpiresTime),
		"last_used_at":      nilIfEmpty(c.LastUsedAt),
		"revoked_at":        nilIfEmpty(c.RevokedAt),
	}
}

func announcedServiceRow(item map[string]any) map[string]any {
	return map[string]any{
		"name":           optString(item, "name"),
		"port":           item["port"],
		"service_type":   optString(item, "serviceType"),
		"service_path":   optString(item, "servicePath"),
		"transport_type": optString(item, "transportType"),
	}
}

const unknownBridgeListNote = `An unknown <bridge-id> prints nothing and exits 0: the API answers it with
an empty list.`

var bridgesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tunnel bridges (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		return listPages(cmd, c, pageSpec[bridge]{
			Route: "GET " + bridgesPath,
			Fetch: getPage(cmd.Context(), bridgesPath, nil),
			Parse: parseList[bridge],
			ID:    func(it bridge) string { return it.ID },
			Rows:  rowFn(bridgeRow),
		})
	},
}

var bridgesGetCmd = &cobra.Command{
	Use:   "get <bridge-id>",
	Short: "Get a single tunnel bridge (pretty JSON)",
	Long:  `Get a single tunnel bridge (pretty JSON). A deleted or unknown bridge exits 4.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return getResource(cmd, bridgePath(args[0]), nil, "id", "bridge")
	},
}

var bridgesCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a tunnel bridge",
	Long: `Create a tunnel bridge.

Prints the bridge as pretty JSON, unwrapped like "bridges get": the id is .id.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		displayName, _ := cmd.Flags().GetString("display-name")
		body := map[string]any{"displayName": displayName}
		if flagGiven(cmd, "description") {
			body["description"], _ = cmd.Flags().GetString("description")
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: bridgesPath, Body: body, Unwrap: named("bridge")})
	},
}

var bridgesUpdateSpec = &updateSpec{fields: []updateField{
	strUpdate("display-name", "displayName"),
	strUpdate("description", "description"),
}}

var bridgesUpdateCmd = &cobra.Command{
	Use:   "update <bridge-id>",
	Short: "Update a tunnel bridge's display name or description",
	Long: `Update a tunnel bridge's display name or description. Only the fields you
pass change. An unknown bridge exits 4.

Prints the bridge as pretty JSON, unwrapped like "bridges get": the id is .id.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// The route takes the fields at the top level, not under "bridge".
		body := map[string]any{}
		mask, err := bridgesUpdateSpec.buildUpdateMask(cmd, body)
		if err != nil {
			return err
		}
		setUpdateMask(body, mask)
		return sendMutation(cmd, mutation{Method: http.MethodPatch, Path: bridgePath(args[0]), Body: body, Unwrap: named("bridge")})
	},
}

var bridgesDeleteCmd = &cobra.Command{
	Use:   "delete <bridge-id>",
	Short: "Delete a tunnel bridge",
	Long: `Delete a tunnel bridge.

Prints the API's response, which is {}. The API also answers {} (exit 0) for
a bridge that is already deleted or does not exist.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: bridgePath(args[0])})
	},
}

var bridgesServicesCmd = &cobra.Command{
	Use:   "services <bridge-id>",
	Short: "List the services a bridge's appliance announces (NDJSON output)",
	Long: `List the services a bridge's appliance announces (NDJSON output). Rows are
name, port, service_type, service_path and transport_type.

` + unknownBridgeListNote,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return getFixedList(cmd, bridgePath(args[0])+"/announced_services", announcedServiceRow)
	},
}

var bridgeCredentialsCmd = &cobra.Command{
	Use:     "credentials",
	Aliases: []string{"credential", "creds"},
	Short:   "Manage a tunnel bridge's client credentials",
}

var bridgeCredentialsListCmd = &cobra.Command{
	Use:   "list <bridge-id>",
	Short: "List a bridge's client credentials (NDJSON output)",
	Long: `List a bridge's client credentials (NDJSON output). A revoked credential is
listed, with credential_status TUNNEL_CREDENTIAL_STATUS_REVOKED.

` + unknownBridgeListNote,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		path := bridgeCredentialsPath(args[0])
		return listPages(cmd, c, pageSpec[bridgeCredential]{
			Route: "GET " + path,
			Fetch: getPage(cmd.Context(), path, nil),
			Parse: parseList[bridgeCredential],
			ID:    func(it bridgeCredential) string { return it.ID },
			Rows:  rowFn(bridgeCredentialRow),
		})
	},
}

var bridgeCredentialsCreateCmd = &cobra.Command{
	Use:   "create <bridge-id>",
	Short: "Create a bridge client credential (the secret is shown once, at creation)",
	Long: `Create a bridge client credential. The secret is shown once, at creation.

Prints the credential as pretty JSON: the id is .id, and .clientId and
.clientSecret sit beside it. Capture .clientSecret now; "bridges credentials
list" does not return it. A deleted or unknown bridge exits 4.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: bridgeCredentialsPath(args[0]), Body: map[string]any{}},
			secretBearing(secretBridgeCredential))
	},
}

var bridgeCredentialsRevokeCmd = &cobra.Command{
	Use:   "revoke <credential-id>",
	Short: "Revoke a bridge client credential",
	Long: `Revoke a bridge client credential.

Prints the API's response, which is {}; revoking an already revoked
credential also answers {} (exit 0). An unknown credential exits 4.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: client.Path("/api/v1/iam/tunnel/credentials/%s", args[0])})
	},
}

func init() {
	rootCmd.AddCommand(bridgesCmd)

	addPaginationFlags(bridgesListCmd)
	addPaginationFlags(bridgeCredentialsListCmd)
	addLimitFlag(bridgesServicesCmd)

	bridgesCreateCmd.Flags().String("display-name", "", "Display name for the new bridge ("+displayNameRule+")")
	bridgesCreateCmd.Flags().String("description", "", "Description")
	markRequired(bridgesCreateCmd, "display-name")

	bridgesUpdateCmd.Flags().String("display-name", "", "New display name (omit to leave unchanged; "+displayNameRule+")")
	bridgesUpdateCmd.Flags().String("description", "", `New description; "" is sent as an empty value (omit to leave unchanged)`)
	allowEmpty(bridgesUpdateCmd, emptyIsSent, "description")
	registerUpdateSpec(bridgesUpdateCmd, bridgesUpdateSpec)

	for _, c := range []*cobra.Command{bridgesCreateCmd, bridgesUpdateCmd, bridgesDeleteCmd, bridgeCredentialsCreateCmd, bridgeCredentialsRevokeCmd} {
		markMutating(c)
	}

	bridgeCredentialsCmd.AddCommand(bridgeCredentialsListCmd, bridgeCredentialsCreateCmd, bridgeCredentialsRevokeCmd)
	bridgesCmd.AddCommand(bridgesListCmd, bridgesGetCmd, bridgesCreateCmd, bridgesUpdateCmd, bridgesDeleteCmd, bridgesServicesCmd, bridgeCredentialsCmd)
}
