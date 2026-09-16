package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var spBindingsCmd = &cobra.Command{
	Use:   "bindings",
	Short: "Manage service principal bindings (which subject acts as a principal)",
	Long: `Bind a subject (a function, SSO application, AuthZEN server, or edge) to a
service principal so it authenticates as that principal.

This surface is a draft API and may change.`,
}

// spBinding is one binding row (a subject's link to one service principal).
type spBinding struct {
	ServicePrincipal string `json:"servicePrincipalId"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
}

func spBindingRow(b spBinding) map[string]any {
	return map[string]any{
		"service_principal_id": b.ServicePrincipal,
		"created_at":           nilIfEmpty(b.CreatedAt),
		"updated_at":           nilIfEmpty(b.UpdatedAt),
	}
}

var spBindingsAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Bind a subject to a service principal (idempotent)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "service-principal-id"); err != nil {
			return err
		}
		subject, err := bindingSubject(cmd)
		if err != nil {
			return err
		}
		spID, _ := cmd.Flags().GetString("service-principal-id")
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		body := map[string]any{"subject": subject, "servicePrincipalId": spID}
		if dryRunActive() {
			return printDryRun(cmd, "POST", spBindingsPath, body)
		}

		c, err := newServicePrincipalsClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		if _, err := c.Post(cmd.Context(), spBindingsPath, body); err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Bound subject to service principal: id=%s\n", spID)
		return nil
	},
}

var spBindingsDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Remove a subject's binding to a service principal (idempotent)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "service-principal-id"); err != nil {
			return err
		}
		subject, err := bindingSubject(cmd)
		if err != nil {
			return err
		}
		spID, _ := cmd.Flags().GetString("service-principal-id")
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		body := map[string]any{"subject": subject, "servicePrincipalId": spID}
		if dryRunActive() {
			return printDryRun(cmd, "POST", spBindingsDeletePath, body)
		}

		c, err := newServicePrincipalsClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		if _, err := c.Post(cmd.Context(), spBindingsDeletePath, body); err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed binding to service principal: id=%s\n", spID)
		return nil
	},
}

var spBindingsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the service principals a subject is bound to (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		subject, err := bindingSubject(cmd)
		if err != nil {
			return err
		}
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
			body := map[string]any{"subject": subject, "pageSize": pageSize}
			if pageToken != "" {
				body["pageToken"] = pageToken
			}

			data, err := c.Post(cmd.Context(), spBindingsListPath, body)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				Bindings      []spBinding `json:"bindings"`
				NextPageToken string      `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, b := range resp.Bindings {
				_ = enc.Encode(spBindingRow(b))
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

func init() {
	servicePrincipalsCmd.AddCommand(spBindingsCmd)

	for _, c := range []*cobra.Command{spBindingsAddCmd, spBindingsDeleteCmd} {
		c.Flags().String("service-principal-id", "", "Service principal ID (required)")
		markRequired(c, "service-principal-id")
		addBindingSubjectFlags(c)
	}

	addBindingSubjectFlags(spBindingsListCmd)
	addPaginationFlags(spBindingsListCmd)

	spBindingsCmd.AddCommand(spBindingsAddCmd, spBindingsListCmd, spBindingsDeleteCmd)
}
