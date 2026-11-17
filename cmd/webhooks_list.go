package cmd

import (
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
		"id":               nilIfEmpty(w.ID),
		"display_name":     nilIfEmpty(w.DisplayName),
		"url":              nilIfEmpty(w.URL),
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
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		return listPages(cmd, c, pageSpec[webhookListItem]{
			Route: "GET " + webhooksPath,
			Fetch: getPage(cmd.Context(), webhooksPath, nil),
			Parse: parseList[webhookListItem],
			ID:    func(it webhookListItem) string { return it.ID },
			Rows:  rowFn(webhookRow),
		})
	},
}

func init() {
	addPaginationFlags(webhooksListCmd)
	webhooksCmd.AddCommand(webhooksListCmd)
}
