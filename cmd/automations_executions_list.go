package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

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
		"id":                     e.ID,
		"automation_template_id": e.AutomationTemplateID,
		"state":                  e.State,
		"created_at":             e.CreatedAt,
		"completed_at":           nilIfEmpty(e.CompletedAt),
		"duration":               e.Duration,
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
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newListClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		stateFilter := mapExecutionState(strings.TrimSpace(cmd.Flag("state").Value.String()))
		templateID, _ := cmd.Flags().GetString("template-id")
		requestedPageSize := pageSizeFlag(cmd)
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		// --state/--template-id filter client-side, so a fetched row is not
		// necessarily written. --fields can also drop a fetched row (see
		// emitter.Filtered). Either way, effectivePageSize must not shrink the
		// per-call page toward the written count while paging past rows that
		// never get written (request amplification); only tighten when neither
		// is active.
		clientFilter := stateFilter != "" || templateID != ""

		enc := newEmitter(cmd)
		guard := newPaginationCursorGuard()
		for !limitReached(enc.Written(), limit) {
			if guard.tokenAlreadyUsed(pageToken) {
				return guard.warnStuck(cmd, "POST /api/v1/search/automation_executions")
			}

			pageSize := requestedPageSize
			if !clientFilter && !enc.Filtered() {
				pageSize = effectivePageSize(requestedPageSize, limit, enc.Written())
			}
			body := map[string]any{"pageSize": pageSize}
			if pageToken != "" {
				body["pageToken"] = pageToken
			}

			// The search route, not GET /api/v1/automation_executions: the GET
			// ignores page_size/page_token and serves page 1 forever.
			data, err := c.Post(cmd.Context(), "/api/v1/search/automation_executions", body)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var page struct {
				List []struct {
					AutomationExecution *executionListItem `json:"automationExecution"`
				} `json:"list"`
				NextPageToken string `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &page); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}
			// A missing wrapper counts as an id-less row, so a page of only
			// those fails as an unrecognized shape instead of printing nothing.
			ids := make([]string, len(page.List))
			for i, item := range page.List {
				if item.AutomationExecution != nil {
					ids[i] = item.AutomationExecution.ID
				}
			}
			keep, err := guard.checkPage(cmd, "POST /api/v1/search/automation_executions", ids)
			if err != nil {
				return err
			}
			// A missing wrapper is skipped rather than emitted as an all-"" row.
			execs := make([]executionListItem, 0, len(page.List))
			for _, item := range keepRows(page.List, keep) {
				if item.AutomationExecution != nil {
					execs = append(execs, *item.AutomationExecution)
				}
			}

			for _, e := range execs {
				if stateFilter != "" && e.State != stateFilter {
					continue
				}
				if templateID != "" && e.AutomationTemplateID != templateID {
					continue
				}
				_ = enc.Encode(executionRow(e))
				if limitReached(enc.Written(), limit) {
					return nil
				}
			}

			if page.NextPageToken == "" || manualPaging {
				break
			}
			guard.markTokenUsed(pageToken)
			pageToken = page.NextPageToken
		}

		return nil
	},
}

// mapExecutionState translates the user-friendly --state value to the bare
// enum the API expects. The full enum name (AUTOMATION_EXECUTION_STATE_DONE)
// is verbose; the short form (done|error|pending) is what humans actually
// remember. Case-insensitive. Returns the input unchanged for unknown values
// so a future enum addition surfaces as a server-side validation error
// rather than a silent client-side drop.
func mapExecutionState(s string) string {
	switch strings.ToLower(s) {
	case "":
		return ""
	case "done", "success":
		return "AUTOMATION_EXECUTION_STATE_DONE"
	case "error", "failed", "failure":
		return "AUTOMATION_EXECUTION_STATE_ERROR"
	case "pending":
		return "AUTOMATION_EXECUTION_STATE_PENDING"
	case "creating":
		return "AUTOMATION_EXECUTION_STATE_CREATING"
	case "waiting":
		return "AUTOMATION_EXECUTION_STATE_WAITING"
	case "terminate", "terminated":
		return "AUTOMATION_EXECUTION_STATE_TERMINATE"
	default:
		return s
	}
}

func init() {
	automationsExecutionsListCmd.Flags().String("state", "", "Filter by execution state: done, error, pending, creating, waiting, terminate (or the full AUTOMATION_EXECUTION_STATE_* enum)")
	automationsExecutionsListCmd.Flags().String("template-id", "", "Filter by automation template (the parent automation's ID)")
	addPaginationFlags(automationsExecutionsListCmd)
	automationsExecutionsCmd.AddCommand(automationsExecutionsListCmd)
}
