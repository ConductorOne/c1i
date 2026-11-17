package cmd

import (
	"fmt"

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
		"id":                  nilIfEmpty(f.ID),
		"display_name":        nilIfEmpty(f.DisplayName),
		"description":         nilIfEmpty(f.Description),
		"function_type":       nilIfEmpty(f.FunctionType),
		"published_commit_id": nilIfEmpty(f.PublishedCommitID),
		"head":                nilIfEmpty(f.Head),
		"is_draft":            f.IsDraft,
		"use_spn":             f.UseSpn,
		"deleted_at":          nilIfEmpty(f.DeletedAt),
	}
}

var functionsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List C1 functions (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		publishedOnly, _ := cmd.Flags().GetBool("published-only")
		draftOnly, _ := cmd.Flags().GetBool("draft-only")
		if publishedOnly && draftOnly {
			return &usageError{fmt.Errorf("--published-only and --draft-only are mutually exclusive")}
		}
		return listPages(cmd, c, pageSpec[functionListItem]{
			Route: "GET /api/v1/functions",
			Fetch: getPage(cmd.Context(), "/api/v1/functions", nil),
			Parse: parseList[functionListItem],
			ID:    func(f functionListItem) string { return f.ID },
			// --published-only/--draft-only filter client-side, so a fetched row is
			// not necessarily written: don't tighten the page toward --limit.
			FixedSize: publishedOnly || draftOnly,
			Rows: func(f functionListItem) []any {
				published := functionIsPublished(f)
				if (publishedOnly && !published) || (draftOnly && published) {
					return nil
				}
				return []any{functionRow(f)}
			},
		})
	},
}

func init() {
	functionsListCmd.Flags().Bool("published-only", false, "Only include functions with a published commit (excludes drafts)")
	functionsListCmd.Flags().Bool("draft-only", false, "Only include functions that are drafts or have never been published")
	addPaginationFlags(functionsListCmd)
	functionsCmd.AddCommand(functionsListCmd)
}
