package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

// functionListItem is one row of GET /api/v1/functions.
type functionListItem struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName"`
	Description       string `json:"description"`
	FunctionType      string `json:"functionType"`
	PublishedCommitID string `json:"publishedCommitId"`
	Head              string `json:"head"`
	IsDraft           bool   `json:"isDraft"`
	UseSpn            bool   `json:"useSpn"`
	DeletedAt         string `json:"deletedAt"`
}

// functionIsPublished reports whether a function has a live published commit
// (as opposed to a draft or a function that has never been published).
func functionIsPublished(f functionListItem) bool {
	return f.PublishedCommitID != "" && !f.IsDraft
}

// functionRow flattens a functionListItem into the NDJSON output row.
// published_commit_id and deleted_at are nil, not "", when unset/live.
func functionRow(f functionListItem) map[string]any {
	return map[string]any{
		"id":                  f.ID,
		"display_name":        f.DisplayName,
		"description":         f.Description,
		"function_type":       f.FunctionType,
		"published_commit_id": nilIfEmpty(f.PublishedCommitID),
		"head":                f.Head,
		"is_draft":            f.IsDraft,
		"use_spn":             f.UseSpn,
		"deleted_at":          nilIfEmpty(f.DeletedAt),
	}
}

var functionsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List C1 functions (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newListClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		publishedOnly, _ := cmd.Flags().GetBool("published-only")
		draftOnly, _ := cmd.Flags().GetBool("draft-only")
		if publishedOnly && draftOnly {
			return &usageError{fmt.Errorf("--published-only and --draft-only are mutually exclusive")}
		}

		requestedPageSize := clampPageSize(getIntFlag(cmd, "page-size"))
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		// --published-only/--draft-only filter client-side, so a fetched row
		// is not necessarily written. --fields can also drop a fetched row (see
		// emitter.Filtered). Either way, effectivePageSize must not shrink the
		// per-call page toward the written count while paging past rows that
		// never get written (request amplification); only tighten when neither
		// is active.
		clientFilter := publishedOnly || draftOnly

		enc := newEmitter(cmd)
		for !limitReached(enc.Written(), limit) {
			pageSize := requestedPageSize
			if !clientFilter && !enc.Filtered() {
				pageSize = effectivePageSize(requestedPageSize, limit, enc.Written())
			}
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
				List          []functionListItem `json:"list"`
				NextPageToken string             `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, f := range resp.List {
				published := functionIsPublished(f)
				if publishedOnly && !published {
					continue
				}
				if draftOnly && published {
					continue
				}
				_ = enc.Encode(functionRow(f))
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
	functionsListCmd.Flags().Bool("published-only", false, "Only include functions with a published commit (excludes drafts)")
	functionsListCmd.Flags().Bool("draft-only", false, "Only include functions that are drafts or have never been published")
	functionsListCmd.Flags().Int("page-size", 50, "Results per page (max 100)")
	functionsListCmd.Flags().String("page-token", "", "Pagination cursor")
	addLimitFlag(functionsListCmd)
	functionsCmd.AddCommand(functionsListCmd)
}
