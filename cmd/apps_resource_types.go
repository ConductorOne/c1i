package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var appsResourceTypesCmd = &cobra.Command{
	Use:   "resource-types",
	Short: "Inspect an app's resource types",
}

// resourceTypeListItem is one row of GET /api/v1/apps/{app-id}/resource_types.
type resourceTypeListItem struct {
	AppResourceType struct {
		ID          string   `json:"id"`
		DisplayName string   `json:"displayName"`
		TraitIDs    []string `json:"traitIds"`
	} `json:"appResourceType"`
}

// resourceTypeRow flattens a resourceTypeListItem into the NDJSON output row.
// trait_ids is nil, not [], on a type with no traits.
func resourceTypeRow(item resourceTypeListItem) map[string]any {
	t := item.AppResourceType
	var traits any
	if len(t.TraitIDs) > 0 {
		traits = t.TraitIDs
	}
	return map[string]any{
		"id":           nilIfEmpty(t.ID),
		"display_name": nilIfEmpty(t.DisplayName),
		"trait_ids":    traits,
	}
}

var appsResourceTypesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List an app's resource types (NDJSON output)",
	Long: `List the resource types of an app (NDJSON output): the id to pass as
--resource-type-id to "apps managed-state". A connector app's discovered apps
are in its resource type named "app".`,
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newListClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		appID, _ := cmd.Flags().GetString("app-id")
		path := client.Path("/api/v1/apps/%s/resource_types", appID)
		requestedPageSize := pageSizeFlag(cmd)
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		enc := newEmitter(cmd)
		guard := newPaginationCursorGuard()
		route := "GET " + path
		for !limitReached(enc.Written(), limit) {
			if guard.tokenAlreadyUsed(pageToken) {
				return guard.warnStuck(cmd, route)
			}
			pageSize := requestedPageSize
			if !enc.Filtered() {
				pageSize = effectivePageSize(requestedPageSize, limit, enc.Written())
			}
			params := map[string]string{"page_size": strconv.Itoa(pageSize)}
			if pageToken != "" {
				params["page_token"] = pageToken
			}

			data, err := c.Get(cmd.Context(), path, params)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List          []resourceTypeListItem `json:"list"`
				NextPageToken string                 `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			ids := make([]string, len(resp.List))
			for i, it := range resp.List {
				ids[i] = it.AppResourceType.ID
			}
			keep, err := guard.checkPage(cmd, route, ids)
			if err != nil {
				return err
			}
			resp.List = keepRows(resp.List, keep)
			for _, item := range resp.List {
				if err := enc.Encode(resourceTypeRow(item)); err != nil {
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
	appsResourceTypesListCmd.Flags().String("app-id", "", "Application ID")
	addPaginationFlags(appsResourceTypesListCmd)
	markRequired(appsResourceTypesListCmd, "app-id")
	appsResourceTypesCmd.AddCommand(appsResourceTypesListCmd)
	appsCmd.AddCommand(appsResourceTypesCmd)
}
