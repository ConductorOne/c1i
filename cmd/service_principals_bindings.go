package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"

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
	Long: `Bind a subject to a service principal (idempotent).

Prints the API's response, which is {}; exit 0 is the success signal.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		subject, err := bindingSubject(cmd)
		if err != nil {
			return err
		}
		spID, _ := cmd.Flags().GetString("service-principal-id")
		body := map[string]any{"subject": subject, "servicePrincipalId": spID}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: spBindingsPath, Body: body})
	},
}

var spBindingsDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Remove a subject's binding to a service principal (idempotent)",
	Long: `Remove a subject's binding to a service principal (idempotent).

Prints the API's response, which is {}; exit 0 is the success signal.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		subject, err := bindingSubject(cmd)
		if err != nil {
			return err
		}
		spID, _ := cmd.Flags().GetString("service-principal-id")
		body := map[string]any{"subject": subject, "servicePrincipalId": spID}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: spBindingsDeletePath, Body: body})
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
		// Rows carry no id of their own, so only the token and idle-page checks apply.
		guard := newPaginationCursorGuard()
		route := "POST " + spBindingsListPath
		for !limitReached(enc.Written(), limit) {
			if guard.tokenAlreadyUsed(pageToken) {
				return guard.warnStuck(cmd, route)
			}
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
			if err := guard.checkIDlessPage(cmd, route, len(resp.Bindings)); err != nil {
				return err
			}

			for _, b := range resp.Bindings {
				if err := enc.Encode(spBindingRow(b)); err != nil {
					return err
				}
				if limitReached(enc.Written(), limit) {
					return nil
				}
			}

			if resp.NextPageToken == "" || manualPaging {
				break
			}
			guard.markTokenUsed(pageToken)
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
		markMutating(c)
	}

	addBindingSubjectFlags(spBindingsListCmd)
	addPaginationFlags(spBindingsListCmd)

	spBindingsCmd.AddCommand(spBindingsAddCmd, spBindingsListCmd, spBindingsDeleteCmd)
}
