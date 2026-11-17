package cmd

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

var spCredentialsCmd = &cobra.Command{
	Use:     "credentials",
	Aliases: []string{"credential", "creds"},
	Short:   "Manage a service principal's client credentials",
}

// spCredential is the subset of ServicePrincipalCredential surfaced in list rows.
type spCredential struct {
	ID               string   `json:"id"`
	ServicePrincipal string   `json:"servicePrincipalId"`
	DisplayName      string   `json:"displayName"`
	ClientID         string   `json:"clientId"`
	CreatedAt        string   `json:"createdAt"`
	ExpiresAt        string   `json:"expiresAt"`
	LastUsedAt       string   `json:"lastUsedAt"`
	RequireDPoP      bool     `json:"requireDpop"`
	ScopedRoleIDs    []string `json:"scopedRoleIds"`
	AllowSourceCIDRs []string `json:"allowSourceCidrs"`
}

// spCredentialRow flattens a credential into an NDJSON output row. The secret
// is never part of this — it is returned only once, by create.
func spCredentialRow(c spCredential) map[string]any {
	return map[string]any{
		"id":                   nilIfEmpty(c.ID),
		"service_principal_id": nilIfEmpty(c.ServicePrincipal),
		"display_name":         nilIfEmpty(c.DisplayName),
		"client_id":            nilIfEmpty(c.ClientID),
		"require_dpop":         c.RequireDPoP,
		"scoped_role_ids":      c.ScopedRoleIDs,
		"allow_source_cidrs":   c.AllowSourceCIDRs,
		"created_at":           nilIfEmpty(c.CreatedAt),
		"expires_at":           nilIfEmpty(c.ExpiresAt),
		"last_used_at":         nilIfEmpty(c.LastUsedAt),
	}
}

var spCredentialsListCmd = &cobra.Command{
	Use:   "list <sp-id>",
	Short: "List a service principal's client credentials (NDJSON output)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		path := spCredentialsPath(args[0])
		return listPages(cmd, c, pageSpec[spCredential]{
			Route: "GET " + path,
			Fetch: getPage(cmd.Context(), path, nil),
			Parse: parseList[spCredential],
			ID:    func(it spCredential) string { return it.ID },
			Rows:  rowFn(spCredentialRow),
		})
	},
}

var spCredentialsGetCmd = &cobra.Command{
	Use:   "get <credential-id>",
	Short: "Get a single client credential (pretty JSON)",
	Long: `Get a single client credential (pretty JSON).

A revoked credential is still returned, unchanged and with exit 0: no field
marks it revoked. "service-principals credentials list" omits it.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}
		spID, _ := cmd.Flags().GetString("service-principal-id")

		c, err := newServicePrincipalsClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Get(cmd.Context(), spCredentialPath(spID, args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeResource(cmd, data, "id")
	},
}

var spCredentialsCreateCmd = &cobra.Command{
	Use:   "create <sp-id>",
	Short: "Create a client credential (the secret is shown once, at creation)",
	Long: `Create a client credential. The secret is shown once, at creation.

Prints the credential as pretty JSON, unwrapped like
"service-principals credentials get": the id is .id, and .clientId and
.clientSecret sit beside it. Capture .clientSecret now; it cannot be read again.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := buildSPCredentialCreateBody(cmd)
		if err != nil {
			return err
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: spCredentialsPath(args[0]), Body: body},
			secretBearing(secretSPCredential))
	},
}

// buildSPCredentialCreateBody assembles the credential-create body from flags,
// omitting optional fields left unset. Shared by the dry-run path and the send.
func buildSPCredentialCreateBody(cmd *cobra.Command) (map[string]any, error) {
	displayName, _ := cmd.Flags().GetString("display-name")
	body := map[string]any{"displayName": displayName}

	roles, err := repeatableStringFlag(cmd, "scoped-role")
	if err != nil {
		return nil, err
	}
	if len(roles) > 0 {
		body["scopedRoles"] = roles
	}
	cidrs, err := repeatableStringFlag(cmd, "allow-cidr")
	if err != nil {
		return nil, err
	}
	if len(cidrs) > 0 {
		body["allowSourceCidrs"] = cidrs
	}
	if v, ok, err := protoDurationFlag(cmd, "expires"); err != nil {
		return nil, err
	} else if ok {
		body["expires"] = v
	}
	if requireDPoP, _ := cmd.Flags().GetBool("require-dpop"); requireDPoP {
		body["requireDpop"] = true
	}
	return body, nil
}

var spCredentialsUpdateSpec = &updateSpec{
	fields: []updateField{strUpdate("display-name", "displayName")},
	scope:  []string{"service-principal-id"},
}

var spCredentialsUpdateCmd = &cobra.Command{
	Use:   "update <credential-id>",
	Short: "Update a client credential's display name",
	Long: `Update a client credential's display name.

Prints the credential as pretty JSON, unwrapped like
"service-principals credentials get": the id is .id.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		spID, _ := cmd.Flags().GetString("service-principal-id")

		cred := map[string]any{"id": args[0]}
		mask, err := spCredentialsUpdateSpec.build(cmd, cred)
		if err != nil {
			return err
		}
		return sendMutation(cmd, mutation{Method: http.MethodPatch, Path: spCredentialPath(spID, args[0]), Body: wrapUpdate("credential", cred, mask)})
	},
}

var spCredentialsRevokeCmd = &cobra.Command{
	Use:   "revoke <credential-id>",
	Short: "Revoke (delete) a client credential",
	Long: `Revoke (delete) a client credential.

Prints the API's response, which is {}; exit 0 is the success signal.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		spID, _ := cmd.Flags().GetString("service-principal-id")
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: spCredentialPath(spID, args[0])})
	},
}

func init() {
	servicePrincipalsCmd.AddCommand(spCredentialsCmd)

	addPaginationFlags(spCredentialsListCmd)

	// service-principal-id is the parent scope for the single-credential ops.
	for _, c := range []*cobra.Command{spCredentialsGetCmd, spCredentialsUpdateCmd, spCredentialsRevokeCmd} {
		c.Flags().String("service-principal-id", "", "Owning service principal ID")
		markRequired(c, "service-principal-id")
	}

	spCredentialsCreateCmd.Flags().String("display-name", "", "Display name for the new credential ("+displayNameRule+")")
	addRepeatableStringFlag(spCredentialsCreateCmd, "scoped-role", "Restrict the credential to a role ID from \"c1i roles list\" (repeatable)")
	addRepeatableStringFlag(spCredentialsCreateCmd, "allow-cidr", "Restrict the credential to a source CIDR (repeatable)")
	addProtoDurationFlag(spCredentialsCreateCmd, "expires", "Time until the credential expires as a protobuf duration (2592000s) or a Go duration (720h), sent in seconds; a sign or decimal (-1h, 1.5h) exits 2; the server accepts the range (0s, 4320h] (up to 180 days)")
	spCredentialsCreateCmd.Flags().Bool("require-dpop", false, "Require DPoP proof-of-possession for token exchange")
	markRequired(spCredentialsCreateCmd, "display-name")

	spCredentialsUpdateCmd.Flags().String("display-name", "", "New display name ("+displayNameRule+")")

	registerUpdateSpec(spCredentialsUpdateCmd, spCredentialsUpdateSpec)
	for _, c := range []*cobra.Command{spCredentialsCreateCmd, spCredentialsUpdateCmd, spCredentialsRevokeCmd} {
		markMutating(c)
	}

	spCredentialsCmd.AddCommand(
		spCredentialsListCmd,
		spCredentialsGetCmd,
		spCredentialsCreateCmd,
		spCredentialsUpdateCmd,
		spCredentialsRevokeCmd,
	)
}
