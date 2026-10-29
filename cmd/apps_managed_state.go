package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var appsManagedStateCmd = &cobra.Command{
	Use:   "managed-state",
	Short: "Inspect and promote connector-discovered applications",
}

var appsManagedStateListCmd = &cobra.Command{
	Use:   "list",
	Short: "List connector-discovered application state (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id", "resource-type-id"); err != nil {
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

		appID, _ := cmd.Flags().GetString("app-id")
		resourceTypeID, _ := cmd.Flags().GetString("resource-type-id")
		path := client.Path("/api/v1/apps/%s/resource_types/%s/managed_state_bindings", appID, resourceTypeID)
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
			params := map[string]string{"page_size": fmt.Sprint(pageSize)}
			if pageToken != "" {
				params["page_token"] = pageToken
			}
			data, err := c.Get(cmd.Context(), path, params)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}
			var resp struct {
				List          []json.RawMessage `json:"list"`
				NextPageToken string            `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}
			for _, binding := range resp.List {
				_ = enc.Encode(binding)
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

var appsManagedStateGetCmd = &cobra.Command{
	Use:   "get <resource-id>",
	Short: "Get one connector-discovered application state (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id", "resource-type-id"); err != nil {
			return err
		}
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}
		appID, _ := cmd.Flags().GetString("app-id")
		resourceTypeID, _ := cmd.Flags().GetString("resource-type-id")
		path := managedStateBindingPath(appID, resourceTypeID, args[0])
		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Get(cmd.Context(), path, nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeNamedResource(cmd, data, "appManagementState")
	},
}

var appsManagedStatePromoteCmd = &cobra.Command{
	Use:   "promote <resource-id>",
	Short: "Promote an unmanaged connector-discovered app (pretty JSON)",
	Long: `Promote an unmanaged connector-discovered application into a managed app.
By default, the new app inherits direct user and entitlement owners from the
source connector app. Pass --user-id and/or --entitlement-owner to set owners
explicitly; an entitlement owner uses APP_ID:ENTITLEMENT_ID. Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id", "resource-type-id"); err != nil {
			return err
		}
		body, err := managedStatePromoteBody(cmd)
		if err != nil {
			return &usageError{err}
		}
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}
		appID, _ := cmd.Flags().GetString("app-id")
		resourceTypeID, _ := cmd.Flags().GetString("resource-type-id")
		path := managedStateBindingPath(appID, resourceTypeID, args[0]) + "/promote"
		if dryRunActive() {
			return printDryRun(cmd, "POST", path, body)
		}
		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Post(cmd.Context(), path, body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeNamedResource(cmd, data, "appManagementState")
	},
}

func managedStateBindingPath(appID, resourceTypeID, resourceID string) string {
	return client.Path("/api/v1/apps/%s/resource_types/%s/managed_state_bindings/%s", appID, resourceTypeID, resourceID)
}

func managedStatePromoteBody(cmd *cobra.Command) (map[string]any, error) {
	body := make(map[string]any)
	userIDs, err := repeatableStringFlag(cmd, "user-id")
	if err != nil {
		return nil, err
	}
	if len(userIDs) > 0 {
		body["userIds"] = userIDs
	}
	owners, err := repeatableStringFlag(cmd, "entitlement-owner")
	if err != nil {
		return nil, err
	}
	if len(owners) > 0 {
		refs := make([]map[string]string, 0, len(owners))
		for _, owner := range owners {
			appID, entitlementID, ok := strings.Cut(owner, ":")
			if !ok || strings.TrimSpace(appID) == "" || strings.TrimSpace(entitlementID) == "" || strings.Contains(entitlementID, ":") {
				return nil, fmt.Errorf("invalid --entitlement-owner %q: use APP_ID:ENTITLEMENT_ID", owner)
			}
			refs = append(refs, map[string]string{"appId": appID, "id": entitlementID})
		}
		body["appEntitlementOwnerRefs"] = refs
	}
	return body, nil
}

func init() {
	addPaginationFlags(appsManagedStateListCmd)
	appsManagedStateListCmd.Flags().String("app-id", "", "Connector application ID")
	appsManagedStateListCmd.Flags().String("resource-type-id", "", "Discovered application resource type ID")
	markRequired(appsManagedStateListCmd, "app-id", "resource-type-id")

	for _, cmd := range []*cobra.Command{appsManagedStateGetCmd, appsManagedStatePromoteCmd} {
		cmd.Flags().String("app-id", "", "Connector application ID")
		cmd.Flags().String("resource-type-id", "", "Discovered application resource type ID")
		markRequired(cmd, "app-id", "resource-type-id")
	}
	addRepeatableStringFlag(appsManagedStatePromoteCmd, "user-id", "User ID to assign as an owner (repeatable)")
	addRepeatableStringFlag(appsManagedStatePromoteCmd, "entitlement-owner", "Entitlement owner as APP_ID:ENTITLEMENT_ID (repeatable)")

	appsManagedStateCmd.AddCommand(appsManagedStateListCmd, appsManagedStateGetCmd, appsManagedStatePromoteCmd)
	appsCmd.AddCommand(appsManagedStateCmd)
}
