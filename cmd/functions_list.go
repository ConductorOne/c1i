package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var functionsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List C1 functions (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := client.New(cmd.Context(), baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		publishedOnly, _ := cmd.Flags().GetBool("published-only")
		draftOnly, _ := cmd.Flags().GetBool("draft-only")
		if publishedOnly && draftOnly {
			return fmt.Errorf("--published-only and --draft-only are mutually exclusive")
		}

		requestedPageSize := clampPageSize(getIntFlag(cmd, "page-size"))
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		enc := json.NewEncoder(cmd.OutOrStdout())
		emitted := 0
		for !limitReached(emitted, limit) {
			pageSize := effectivePageSize(requestedPageSize, limit, emitted)
			params := map[string]string{
				"page_size": strconv.Itoa(pageSize),
			}
			if pageToken != "" {
				params["page_token"] = pageToken
			}

			data, err := c.Get(cmd.Context(), "/api/v1/functions", params)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List []struct {
					ID                 string `json:"id"`
					DisplayName        string `json:"displayName"`
					Description        string `json:"description"`
					FunctionType       string `json:"functionType"`
					PublishedCommitID  string `json:"publishedCommitId"`
					Head               string `json:"head"`
					IsDraft            bool   `json:"isDraft"`
					UseSpn             bool   `json:"useSpn"`
				} `json:"list"`
				NextPageToken string `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, f := range resp.List {
				published := f.PublishedCommitID != "" && !f.IsDraft
				if publishedOnly && !published {
					continue
				}
				if draftOnly && published {
					continue
				}
				_ = enc.Encode(map[string]any{
					"id":                  f.ID,
					"display_name":        f.DisplayName,
					"description":         f.Description,
					"function_type":       f.FunctionType,
					"published_commit_id": f.PublishedCommitID,
					"head":                f.Head,
					"is_draft":            f.IsDraft,
					"use_spn":             f.UseSpn,
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
	functionsListCmd.Flags().Bool("published-only", false, "Only include functions with a published commit (excludes drafts)")
	functionsListCmd.Flags().Bool("draft-only", false, "Only include functions that are drafts or have never been published")
	functionsListCmd.Flags().Int("page-size", 50, "Results per page (max 100)")
	functionsListCmd.Flags().String("page-token", "", "Pagination cursor")
	addLimitFlag(functionsListCmd)
	functionsCmd.AddCommand(functionsListCmd)
}
