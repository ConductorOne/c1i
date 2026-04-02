package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ductone/c1i/internal/client"
	"github.com/spf13/cobra"
)

var appsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List applications (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		tenant, err := GetTenant()
		if err != nil {
			return err
		}

		c, err := client.New(cmd.Context(), tenant)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

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

			data, err := c.Get(cmd.Context(), "/api/v1/apps", params)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List []struct {
					ID          string `json:"id"`
					DisplayName string `json:"displayName"`
					Description string `json:"description"`
					UserCount   string `json:"userCount"`
					IconURL     string `json:"iconUrl"`
				} `json:"list"`
				NextPageToken string `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, a := range resp.List {
				_ = enc.Encode(map[string]string{
					"id":           a.ID,
					"display_name": a.DisplayName,
					"description":  a.Description,
					"user_count":   a.UserCount,
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
	appsListCmd.Flags().Int("page-size", 50, "Results per page")
	appsListCmd.Flags().String("page-token", "", "Pagination cursor")
	appsCmd.AddCommand(appsListCmd)
}
