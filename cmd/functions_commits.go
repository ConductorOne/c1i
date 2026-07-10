package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var functionsCommitsCmd = &cobra.Command{
	Use:   "commits <function-id>",
	Short: "List a function's commit history (NDJSON output)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		functionID := args[0]
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		requestedPageSize := clampPageSize(getIntFlag(cmd, "page-size"))
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		enc := newEmitter(cmd.OutOrStdout())
		emitted := 0
		for !limitReached(emitted, limit) {
			pageSize := effectivePageSize(requestedPageSize, limit, emitted)
			params := map[string]string{
				"page_size": strconv.Itoa(pageSize),
			}
			if pageToken != "" {
				params["page_token"] = pageToken
			}

			data, err := c.Get(cmd.Context(), client.Path("/api/v1/functions/%s/commits", functionID), params)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List []struct {
					ID         string `json:"id"`
					FunctionID string `json:"functionId"`
					Author     string `json:"author"`
					Message    string `json:"message"`
					CreatedAt  string `json:"createdAt"`
				} `json:"list"`
				NextPageToken string `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, c := range resp.List {
				_ = enc.Encode(map[string]string{
					"id":          c.ID,
					"function_id": c.FunctionID,
					"author":      c.Author,
					"message":     c.Message,
					"created_at":  c.CreatedAt,
				})
				emitted++
				if limitReached(emitted, limit) {
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
	functionsCommitsCmd.Flags().Int("page-size", 50, "Results per page (max 100)")
	functionsCommitsCmd.Flags().String("page-token", "", "Pagination cursor")
	addLimitFlag(functionsCommitsCmd)
	functionsCmd.AddCommand(functionsCommitsCmd)
}
