package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ductone/c1i/internal/client"
	"github.com/spf13/cobra"
)

var connectorsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List connectors for an application (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := client.New(cmd.Context(), baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		appID, _ := cmd.Flags().GetString("app-id")
		pageSize, _ := cmd.Flags().GetInt("page-size")
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")

		enc := json.NewEncoder(cmd.OutOrStdout())
		for {
			params := map[string]string{
				"page_size": strconv.Itoa(pageSize),
			}
			if pageToken != "" {
				params["page_token"] = pageToken
			}

			path := fmt.Sprintf("/api/v1/apps/%s/connectors", appID)
			data, err := c.Get(cmd.Context(), path, params)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List []struct {
					Connector struct {
						ID          string `json:"id"`
						AppID       string `json:"appId"`
						DisplayName string `json:"displayName"`
						Status      struct {
							Status string `json:"status"`
						} `json:"status"`
					} `json:"connector"`
				} `json:"list"`
				NextPageToken string `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, item := range resp.List {
				conn := item.Connector
				_ = enc.Encode(map[string]string{
					"id":           conn.ID,
					"app_id":       conn.AppID,
					"display_name": conn.DisplayName,
					"status":       conn.Status.Status,
				})
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
	connectorsListCmd.Flags().Int("page-size", 50, "Results per page")
	connectorsListCmd.Flags().String("page-token", "", "Pagination cursor")
	_ = connectorsListCmd.MarkFlagRequired("app-id")
	connectorsCmd.AddCommand(connectorsListCmd)
}
