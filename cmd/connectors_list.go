package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// connectorListItem is one row of GET /api/v1/apps/{appId}/connectors.
type connectorListItem struct {
	Connector struct {
		ID          string `json:"id"`
		AppID       string `json:"appId"`
		DisplayName string `json:"displayName"`
		Status      struct {
			Status string `json:"status"`
		} `json:"status"`
		DeletedAt string `json:"deletedAt"`
	} `json:"connector"`
}

// connectorRow flattens a connectorListItem into the NDJSON output row.
// deleted_at is nil, not "", on a live connector.
func connectorRow(item connectorListItem) map[string]any {
	conn := item.Connector
	return map[string]any{
		"id":           conn.ID,
		"app_id":       conn.AppID,
		"display_name": conn.DisplayName,
		"status":       conn.Status.Status,
		"deleted_at":   nilIfEmpty(conn.DeletedAt),
	}
}

var connectorsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List connectors for an application (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "app-id"); err != nil {
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
		requestedPageSize := clampPageSize(getIntFlag(cmd, "page-size"))
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		enc := newEmitter(cmd)
		for !limitReached(enc.Written(), limit) {
			pageSize := effectivePageSize(requestedPageSize, limit, enc.Written())
			params := map[string]string{
				"page_size": strconv.Itoa(pageSize),
			}
			if pageToken != "" {
				params["page_token"] = pageToken
			}

			path := client.Path("/api/v1/apps/%s/connectors", appID)
			data, err := c.Get(cmd.Context(), path, params)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List          []connectorListItem `json:"list"`
				NextPageToken string              `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, item := range resp.List {
				_ = enc.Encode(connectorRow(item))
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
	connectorsListCmd.Flags().String("app-id", "", "Application ID")
	connectorsListCmd.Flags().Int("page-size", 50, "Results per page (max 100)")
	connectorsListCmd.Flags().String("page-token", "", "Pagination cursor")
	markRequired(connectorsListCmd, "app-id")
	addLimitFlag(connectorsListCmd)
	connectorsCmd.AddCommand(connectorsListCmd)
}
