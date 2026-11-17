package cmd

import (
	"github.com/spf13/cobra"
)

// automationStep captures the subset of the automation step shape used by
// the list/usage commands. We keep this narrow on purpose — the full step
// surface is large (createAccessReview, callFunction, sendEmail, …) and
// listing automations only needs to know which functions they invoke.
type automationStep struct {
	StepName     string `json:"stepName"`
	CallFunction *struct {
		FunctionID string `json:"functionId"`
	} `json:"callFunction"`
}

func stepCallsFunction(steps []automationStep, functionID string) bool {
	for _, s := range steps {
		if s.CallFunction != nil && s.CallFunction.FunctionID == functionID {
			return true
		}
	}
	return false
}

func uniqueFunctionIDs(steps []automationStep) []string {
	var ids []string
	seen := map[string]bool{}
	for _, s := range steps {
		if s.CallFunction == nil || s.CallFunction.FunctionID == "" {
			continue
		}
		if seen[s.CallFunction.FunctionID] {
			continue
		}
		seen[s.CallFunction.FunctionID] = true
		ids = append(ids, s.CallFunction.FunctionID)
	}
	return ids
}

// automationListItem is one row of POST /api/v1/search/automations.
type automationListItem struct {
	ID                 string           `json:"id"`
	DisplayName        string           `json:"displayName"`
	Description        string           `json:"description"`
	Enabled            bool             `json:"enabled"`
	LastExecutedAt     string           `json:"lastExecutedAt"`
	PrimaryTriggerType string           `json:"primaryTriggerType"`
	IsDraft            bool             `json:"isDraft"`
	AutomationSteps    []automationStep `json:"automationSteps"`
}

// automationRow flattens an automationListItem into the NDJSON output row.
// last_executed_at is nil, not "", for an automation that has never run.
func automationRow(a automationListItem) map[string]any {
	return map[string]any{
		"id":                   nilIfEmpty(a.ID),
		"display_name":         nilIfEmpty(a.DisplayName),
		"description":          nilIfEmpty(a.Description),
		"enabled":              a.Enabled,
		"last_executed_at":     nilIfEmpty(a.LastExecutedAt),
		"primary_trigger_type": nilIfEmpty(a.PrimaryTriggerType),
		"is_draft":             a.IsDraft,
		"function_ids":         uniqueFunctionIDs(a.AutomationSteps),
	}
}

var automationsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List automations (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		enabledOnly, _ := cmd.Flags().GetBool("enabled-only")
		callsFunction, _ := cmd.Flags().GetString("calls-function")
		return listPages(cmd, c, pageSpec[automationListItem]{
			Route: "POST /api/v1/search/automations",
			// The search route, not GET /api/v1/automations: the GET ignores
			// page_size/page_token and serves page 1 forever past ~60 rows.
			Fetch: postPage(cmd.Context(), "/api/v1/search/automations", map[string]any{}),
			Parse: parseList[automationListItem],
			ID:    func(a automationListItem) string { return a.ID },
			// --enabled-only/--calls-function filter client-side, so a fetched row is
			// not necessarily written: don't tighten the page toward --limit.
			FixedSize: enabledOnly || callsFunction != "",
			Rows: func(a automationListItem) []any {
				if enabledOnly && !a.Enabled {
					return nil
				}
				if callsFunction != "" && !stepCallsFunction(a.AutomationSteps, callsFunction) {
					return nil
				}
				return []any{automationRow(a)}
			},
		})
	},
}

func init() {
	automationsListCmd.Flags().Bool("enabled-only", false, "Only emit automations that are currently enabled")
	automationsListCmd.Flags().String("calls-function", "", "Only emit automations that invoke the given function ID (across any step)")
	addPaginationFlags(automationsListCmd)
	automationsCmd.AddCommand(automationsListCmd)
}
