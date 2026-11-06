package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

// webhookListItem is the subset of Webhook surfaced in list rows.
type webhookListItem struct {
	ID              string `json:"id"`
	DisplayName     string `json:"displayName"`
	URL             string `json:"url"`
	Description     string `json:"description"`
	CallbackTimeout string `json:"callbackTimeout"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

// webhookRow flattens a webhook into an NDJSON row; absent values are null.
func webhookRow(w webhookListItem) map[string]any {
	return map[string]any{
		"id":               w.ID,
		"display_name":     w.DisplayName,
		"url":              w.URL,
		"description":      nilIfEmpty(w.Description),
		"callback_timeout": nilIfEmpty(w.CallbackTimeout),
		"created_at":       nilIfEmpty(w.CreatedAt),
		"updated_at":       nilIfEmpty(w.UpdatedAt),
	}
}

var webhooksListCmd = &cobra.Command{
	Use:   "list",
	Short: "List webhooks (NDJSON output)",
	Long: `List webhooks (NDJSON output). Deleted webhooks are omitted.

The server raises a page size below 5 to 5 and returns one row more than the
page size it uses, so pages are larger than --page-size asks for; use --limit
for an exact count.`,
	RunE: func(cmd *cobra.Command, args []string) error {
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
		guard := newPaginationCursorGuard()
		route := "GET " + webhooksPath
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

			data, err := c.Get(cmd.Context(), webhooksPath, params)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List          []webhookListItem `json:"list"`
				NextPageToken string            `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			ids := make([]string, len(resp.List))
			for i, it := range resp.List {
				ids[i] = it.ID
			}
			keep, err := guard.checkPage(cmd, route, ids)
			if err != nil {
				return err
			}
			resp.List = keepRows(resp.List, keep)
			for _, w := range resp.List {
				if err := enc.Encode(webhookRow(w)); err != nil {
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
	addPaginationFlags(webhooksListCmd)
	webhooksCmd.AddCommand(webhooksListCmd)
}
