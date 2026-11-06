package cmd

import (
	"encoding/json"
	"fmt"

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
		"id":                   a.ID,
		"display_name":         a.DisplayName,
		"description":          a.Description,
		"enabled":              a.Enabled,
		"last_executed_at":     nilIfEmpty(a.LastExecutedAt),
		"primary_trigger_type": a.PrimaryTriggerType,
		"is_draft":             a.IsDraft,
		"function_ids":         uniqueFunctionIDs(a.AutomationSteps),
	}
}

var automationsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List automations (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newListClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		enabledOnly, _ := cmd.Flags().GetBool("enabled-only")
		callsFunction, _ := cmd.Flags().GetString("calls-function")
		requestedPageSize := pageSizeFlag(cmd)
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		// --enabled-only/--calls-function filter client-side, so a fetched row
		// is not necessarily written. --fields can also drop a fetched row (see
		// emitter.Filtered). Either way, effectivePageSize must not shrink the
		// per-call page toward the written count while paging past rows that
		// never get written (request amplification); only tighten when neither
		// is active.
		clientFilter := enabledOnly || callsFunction != ""

		enc := newEmitter(cmd)
		guard := newPaginationCursorGuard()
		for !limitReached(enc.Written(), limit) {
			if guard.tokenAlreadyUsed(pageToken) {
				return guard.warnStuck(cmd, "POST /api/v1/search/automations")
			}

			pageSize := requestedPageSize
			if !clientFilter && !enc.Filtered() {
				pageSize = effectivePageSize(requestedPageSize, limit, enc.Written())
			}
			body := map[string]any{"pageSize": pageSize}
			if pageToken != "" {
				body["pageToken"] = pageToken
			}

			// The search route, not GET /api/v1/automations: the GET ignores
			// page_size/page_token and serves page 1 forever past ~60 rows.
			data, err := c.Post(cmd.Context(), "/api/v1/search/automations", body)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List          []automationListItem `json:"list"`
				NextPageToken string               `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			ids := make([]string, len(resp.List))
			for i, a := range resp.List {
				ids[i] = a.ID
			}
			keep, err := guard.checkPage(cmd, "POST /api/v1/search/automations", ids)
			if err != nil {
				return err
			}
			resp.List = keepRows(resp.List, keep)

			for _, a := range resp.List {
				if enabledOnly && !a.Enabled {
					continue
				}
				if callsFunction != "" && !stepCallsFunction(a.AutomationSteps, callsFunction) {
					continue
				}

				if err := enc.Encode(automationRow(a)); err != nil {
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
	automationsListCmd.Flags().Bool("enabled-only", false, "Only emit automations that are currently enabled")
	automationsListCmd.Flags().String("calls-function", "", "Only emit automations that invoke the given function ID (across any step)")
	addPaginationFlags(automationsListCmd)
	automationsCmd.AddCommand(automationsListCmd)
}
