package cmd

import (
	"github.com/spf13/cobra"
)

var webhooksSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search webhooks by text or id (NDJSON output)",
	Long: `Search webhooks (POST /api/v1/search/webhooks), one row per webhook with the
same fields as "webhooks list". --query matches the display name (the API says
names and descriptions; a word that occurs only in a description matched
nothing when tried); each --webhook-id restricts the search to that webhook
(repeatable). With neither flag it returns every webhook. Given both, a
webhook must satisfy both.

The search index lags the writes: a new webhook was missing from an unfiltered
search for a few seconds, and a deleted one may still be found briefly. Use "webhooks list" or "webhooks get" for the current state.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		body := map[string]any{}
		if flagGiven(cmd, "query") {
			body["query"] = mustGetString(cmd, "query")
		}
		ids, err := repeatableStringFlag(cmd, "webhook-id")
		if err != nil {
			return err
		}
		if len(ids) > 0 {
			refs := make([]map[string]any, len(ids))
			for i, id := range ids {
				refs[i] = map[string]any{"id": id}
			}
			body["refs"] = refs
		}
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		return listPages(cmd, c, pageSpec[webhookListItem]{
			Route: "POST " + webhooksSearchPath,
			Fetch: postPage(cmd.Context(), webhooksSearchPath, body),
			Parse: parseList[webhookListItem],
			ID:    func(it webhookListItem) string { return it.ID },
			Rows:  rowFn(webhookRow),
		})
	},
}

const webhooksSearchPath = "/api/v1/search/webhooks"

func init() {
	f := webhooksSearchCmd.Flags()
	f.String("query", "", "Text to match against webhook names (a word only in a description matched nothing when tried)")
	addRepeatableStringFlag(webhooksSearchCmd, "webhook-id", "Restrict the search to this webhook id (repeatable)")
	addPaginationFlags(webhooksSearchCmd)
	webhooksCmd.AddCommand(webhooksSearchCmd)
}
