package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

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
		"id":                   c.ID,
		"service_principal_id": c.ServicePrincipal,
		"display_name":         c.DisplayName,
		"client_id":            c.ClientID,
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
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newListClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		requestedPageSize := pageSizeFlag(cmd)
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		enc := newEmitter(cmd)
		for !limitReached(enc.Written(), limit) {
			pageSize := requestedPageSize
			if !enc.Filtered() {
				pageSize = effectivePageSize(requestedPageSize, limit, enc.Written())
			}
			params := map[string]string{"page_size": strconv.Itoa(pageSize)}
			if pageToken != "" {
				params["page_token"] = pageToken
			}

			data, err := c.Get(cmd.Context(), spCredentialsPath(args[0]), params)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List          []spCredential `json:"list"`
				NextPageToken string         `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, cred := range resp.List {
				_ = enc.Encode(spCredentialRow(cred))
				if limitReached(enc.Written(), limit) {
					return nil
				}
			}

			if resp.NextPageToken == "" || manualPaging {
				break
			}
			pageToken = resp.NextPageToken
		}

		return nil
	},
}

var spCredentialsGetCmd = &cobra.Command{
	Use:   "get <credential-id>",
	Short: "Get a single client credential (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "service-principal-id"); err != nil {
			return err
		}
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
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "display-name"); err != nil {
			return err
		}
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		body, err := buildSPCredentialCreateBody(cmd)
		if err != nil {
			return err
		}
		path := spCredentialsPath(args[0])
		if dryRunActive() {
			return printDryRun(cmd, "POST", path, body)
		}

		c, err := newServicePrincipalsClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Post(cmd.Context(), path, body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeRawObject(cmd, data)
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
	if expires, _ := cmd.Flags().GetString("expires"); expires != "" {
		d, err := time.ParseDuration(expires)
		if err != nil {
			return nil, &usageError{fmt.Errorf("invalid --expires %q: %w", expires, err)}
		}
		if d <= 0 {
			return nil, &usageError{fmt.Errorf("--expires must be positive, got %q", expires)}
		}
		// ProtoJSON allows nanosecond precision; fixed-width decimals preserve it.
		body["expires"] = formatProtoJSONDuration(d)
	}
	if requireDPoP, _ := cmd.Flags().GetBool("require-dpop"); requireDPoP {
		body["requireDpop"] = true
	}
	return body, nil
}

func formatProtoJSONDuration(d time.Duration) string {
	seconds := d / time.Second
	nanoseconds := d % time.Second
	if nanoseconds == 0 {
		return fmt.Sprintf("%ds", seconds)
	}
	return fmt.Sprintf("%d.%09ds", seconds, nanoseconds)
}

var spCredentialsUpdateCmd = &cobra.Command{
	Use:   "update <credential-id>",
	Short: "Update a client credential's display name",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "service-principal-id"); err != nil {
			return err
		}
		spID, _ := cmd.Flags().GetString("service-principal-id")

		cred := map[string]any{"id": args[0]}
		var paths []string
		if cmd.Flags().Changed("display-name") {
			v, _ := cmd.Flags().GetString("display-name")
			cred["displayName"] = v
			paths = append(paths, "displayName")
		}
		if len(paths) == 0 {
			return &usageError{fmt.Errorf("nothing to update: pass --display-name")}
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		body := map[string]any{
			"credential": cred,
			"updateMask": strings.Join(paths, ","),
		}
		path := spCredentialPath(spID, args[0])
		if dryRunActive() {
			return printDryRun(cmd, "PATCH", path, body)
		}

		c, err := newServicePrincipalsClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Patch(cmd.Context(), path, body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeRawObject(cmd, data)
	},
}

var spCredentialsRevokeCmd = &cobra.Command{
	Use:   "revoke <credential-id>",
	Short: "Revoke (delete) a client credential",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "service-principal-id"); err != nil {
			return err
		}
		spID, _ := cmd.Flags().GetString("service-principal-id")
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		path := spCredentialPath(spID, args[0])
		if dryRunActive() {
			return printDryRun(cmd, "DELETE", path, nil)
		}

		c, err := newServicePrincipalsClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		if _, err := c.Delete(cmd.Context(), path); err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Revoked credential: id=%s service_principal_id=%s\n", args[0], spID)
		return nil
	},
}

func init() {
	servicePrincipalsCmd.AddCommand(spCredentialsCmd)

	addPaginationFlags(spCredentialsListCmd)

	// service-principal-id is the parent scope for the single-credential ops.
	for _, c := range []*cobra.Command{spCredentialsGetCmd, spCredentialsUpdateCmd, spCredentialsRevokeCmd} {
		c.Flags().String("service-principal-id", "", "Owning service principal ID (required)")
		markRequired(c, "service-principal-id")
	}

	spCredentialsCreateCmd.Flags().String("display-name", "", "Display name for the new credential (required)")
	addRepeatableStringFlag(spCredentialsCreateCmd, "scoped-role", "Restrict the credential to a role ID (repeatable)")
	addRepeatableStringFlag(spCredentialsCreateCmd, "allow-cidr", "Restrict the credential to a source CIDR (repeatable)")
	spCredentialsCreateCmd.Flags().String("expires", "", "Time until the credential expires as a Go duration, e.g. 720h; the server accepts the range (0s, 4320h] (up to 180 days)")
	spCredentialsCreateCmd.Flags().Bool("require-dpop", false, "Require DPoP proof-of-possession for token exchange")
	markRequired(spCredentialsCreateCmd, "display-name")

	spCredentialsUpdateCmd.Flags().String("display-name", "", "New display name")

	spCredentialsCmd.AddCommand(
		spCredentialsListCmd,
		spCredentialsGetCmd,
		spCredentialsCreateCmd,
		spCredentialsUpdateCmd,
		spCredentialsRevokeCmd,
	)
}
