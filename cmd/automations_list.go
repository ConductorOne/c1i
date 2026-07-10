package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

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

var automationsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List automations (NDJSON output)",
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		enabledOnly, _ := cmd.Flags().GetBool("enabled-only")
		callsFunction, _ := cmd.Flags().GetString("calls-function")
		requestedPageSize := clampPageSize(getIntFlag(cmd, "page-size"))
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		// --enabled-only/--calls-function filter client-side, so a fetched row
		// is not necessarily emitted. effectivePageSize tightens based on the
		// emitted count, which would shrink the per-call page toward 1 while
		// paging past non-matching rows (request amplification). Only tighten
		// when no client-side filter is active.
		clientFilter := enabledOnly || callsFunction != ""

		enc := newEmitter(cmd.OutOrStdout())
		emitted := 0
		for !limitReached(emitted, limit) {
			pageSize := requestedPageSize
			if !clientFilter {
				pageSize = effectivePageSize(requestedPageSize, limit, emitted)
			}
			params := map[string]string{
				"page_size": strconv.Itoa(pageSize),
			}
			if pageToken != "" {
				params["page_token"] = pageToken
			}

			data, err := c.Get(cmd.Context(), "/api/v1/automations", params)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List []struct {
					ID                 string           `json:"id"`
					DisplayName        string           `json:"displayName"`
					Description        string           `json:"description"`
					Enabled            bool             `json:"enabled"`
					LastExecutedAt     string           `json:"lastExecutedAt"`
					PrimaryTriggerType string           `json:"primaryTriggerType"`
					IsDraft            bool             `json:"isDraft"`
					AutomationSteps    []automationStep `json:"automationSteps"`
				} `json:"list"`
				NextPageToken string `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, a := range resp.List {
				if enabledOnly && !a.Enabled {
					continue
				}
				if callsFunction != "" && !stepCallsFunction(a.AutomationSteps, callsFunction) {
					continue
				}

				_ = enc.Encode(map[string]any{
					"id":                   a.ID,
					"display_name":         a.DisplayName,
					"description":          a.Description,
					"enabled":              a.Enabled,
					"last_executed_at":     a.LastExecutedAt,
					"primary_trigger_type": a.PrimaryTriggerType,
					"is_draft":             a.IsDraft,
					"function_ids":         uniqueFunctionIDs(a.AutomationSteps),
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
	automationsListCmd.Flags().Bool("enabled-only", false, "Only emit automations that are currently enabled")
	automationsListCmd.Flags().String("calls-function", "", "Only emit automations that invoke the given function ID (across any step)")
	automationsListCmd.Flags().Int("page-size", 50, "Results per page (max 100)")
	automationsListCmd.Flags().String("page-token", "", "Pagination cursor")
	addLimitFlag(automationsListCmd)
	automationsCmd.AddCommand(automationsListCmd)
}
