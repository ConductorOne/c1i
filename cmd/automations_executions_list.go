package cmd

import (
	"github.com/spf13/cobra"
)

// executionListItem is one row of POST /api/v1/search/automation_executions.
type executionListItem struct {
	ID                   string `json:"id"`
	AutomationTemplateID string `json:"automationTemplateId"`
	State                string `json:"state"`
	CreatedAt            string `json:"createdAt"`
	CompletedAt          string `json:"completedAt"`
	Duration             string `json:"duration"`
	IsDraft              bool   `json:"isDraft"`
}

// executionRow flattens an executionListItem into the NDJSON output row.
// completed_at is nil, not "", for a non-terminal execution (pending,
// creating, waiting).
func executionRow(e executionListItem) map[string]any {
	return map[string]any{
		"id":                     nilIfEmpty(e.ID),
		"automation_template_id": nilIfEmpty(e.AutomationTemplateID),
		"state":                  nilIfEmpty(e.State),
		"created_at":             nilIfEmpty(e.CreatedAt),
		"completed_at":           nilIfEmpty(e.CompletedAt),
		"duration":               nilIfEmpty(e.Duration),
		"is_draft":               e.IsDraft,
	}
}

var automationsExecutionsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List automation executions (NDJSON output)",
	Long: `Stream automation execution history.

Filters --state and --template-id are applied client-side after fetching
each page, so a narrow filter still scans every page the server returns.
Combine with --limit to bound the work.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		// Empty means no filter; whitespace-only reaches the parser, which
		// rejects a blank key rather than silently dropping the filter.
		stateFilter, err := executionStateFlag.parse(mustGetString(cmd, "state"))
		if err != nil {
			return err
		}
		templateID, _ := cmd.Flags().GetString("template-id")
		return listPages(cmd, c, pageSpec[executionSearchItem]{
			Route: "POST /api/v1/search/automation_executions",
			// The search route, not GET /api/v1/automation_executions: the GET
			// ignores page_size/page_token and serves page 1 forever.
			Fetch: postPage(cmd.Context(), "/api/v1/search/automation_executions", map[string]any{}),
			Parse: parseList[executionSearchItem],
			// The spec allows a null wrapper; such an item is an id-less row to the
			// guard and is skipped rather than emitted as an all-"" row.
			ID: func(it executionSearchItem) string {
				if it.AutomationExecution == nil {
					return ""
				}
				return it.AutomationExecution.ID
			},
			Composite: true,
			// --state/--template-id filter client-side, so a fetched row is not
			// necessarily written: don't tighten the page toward --limit.
			FixedSize: stateFilter != "" || templateID != "",
			Rows: func(it executionSearchItem) []any {
				e := it.AutomationExecution
				if e == nil || (stateFilter != "" && e.State != stateFilter) || (templateID != "" && e.AutomationTemplateID != templateID) {
					return nil
				}
				return []any{executionRow(*e)}
			},
		})
	},
}

// executionStateFlag filters client-side, so an unknown short name used to
// match nothing and exit 0; it is now refused. A prefixed value still passes,
// for a state newer than c1i.
var executionStateFlag = enumFlag{flag: "state", prefix: "AUTOMATION_EXECUTION_STATE_", unknown: enumPassPrefixed, values: []enumValue{
	{name: "done", api: "AUTOMATION_EXECUTION_STATE_DONE", aliases: []string{"success"}},
	{name: "error", api: "AUTOMATION_EXECUTION_STATE_ERROR", aliases: []string{"failed", "failure"}},
	{name: "pending", api: "AUTOMATION_EXECUTION_STATE_PENDING"},
	{name: "creating", api: "AUTOMATION_EXECUTION_STATE_CREATING"},
	{name: "waiting", api: "AUTOMATION_EXECUTION_STATE_WAITING"},
	{name: "terminate", api: "AUTOMATION_EXECUTION_STATE_TERMINATE", aliases: []string{"terminated"}},
}}

// executionSearchItem is one row of the search response; the wrapper may be null.
type executionSearchItem struct {
	AutomationExecution *executionListItem `json:"automationExecution"`
}

func init() {
	automationsExecutionsListCmd.Flags().String("state", "", "Filter by execution state: "+executionStateFlag.usage())
	automationsExecutionsListCmd.Flags().String("template-id", "", "Filter by automation template (the parent automation's ID)")
	addPaginationFlags(automationsExecutionsListCmd)
	automationsExecutionsCmd.AddCommand(automationsExecutionsListCmd)
}
