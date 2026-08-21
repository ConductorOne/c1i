package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

var functionsUsageCmd = &cobra.Command{
	Use:   "usage <function-id>",
	Short: "List automations that invoke this function (NDJSON output)",
	Long: `Scan automations in the tenant and emit one row per automation step
that calls this function. Useful for "is this function still used?"
audits before deleting a draft, or to find an example automation that
exercises a published function.

Reads /api/v1/automations and inspects automationSteps[].callFunction.
NDJSON fields: automation_id, automation_name, step_name, enabled,
last_executed_at, args.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		functionID := args[0]
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newListClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		// Page size tracks --page-size alone, unlike the effectivePageSize
		// shrink other list commands apply toward --limit. That shrink
		// assumes rows fetched and rows written are 1:1; this command
		// filters automations client-side by callFunction.functionId (and a
		// --fields projection can drop a row too), so a fetched row here is
		// not necessarily written. Feeding that into effectivePageSize would
		// send page_size=1 for every automation scanned until a match turns
		// up — one HTTP round trip per automation instead of a handful of
		// batched pages. Any list command that filters after the fetch
		// inherits this same trap (see emitter.Filtered).
		pageSize := clampPageSize(getIntFlag(cmd, "page-size"))
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		enc := newEmitter(cmd)
		// emitted counts automations that call functionID (the client-side
		// filter above), used only for the "no automations call function"
		// message below. The stop condition uses enc.Written() instead: a
		// --fields projection can also drop a fetched-and-filter-matched row
		// (see emitter.Filtered), so emitted alone would overcount rows
		// actually written to stdout.
		emitted := 0
		for !limitReached(enc.Written(), limit) {
			params := map[string]string{"page_size": strconv.Itoa(pageSize)}
			if pageToken != "" {
				params["page_token"] = pageToken
			}

			data, err := c.Get(cmd.Context(), "/api/v1/automations", params)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List []struct {
					ID              string `json:"id"`
					DisplayName     string `json:"displayName"`
					Enabled         bool   `json:"enabled"`
					LastExecutedAt  string `json:"lastExecutedAt"`
					AutomationSteps []struct {
						StepName     string `json:"stepName"`
						CallFunction *struct {
							FunctionID string         `json:"functionId"`
							Args       map[string]any `json:"args"`
						} `json:"callFunction"`
					} `json:"automationSteps"`
				} `json:"list"`
				NextPageToken string `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, a := range resp.List {
				for _, step := range a.AutomationSteps {
					if step.CallFunction == nil || step.CallFunction.FunctionID != functionID {
						continue
					}
					argKeys := make([]string, 0, len(step.CallFunction.Args))
					for k := range step.CallFunction.Args {
						argKeys = append(argKeys, k)
					}
					_ = enc.Encode(map[string]any{
						"automation_id":    a.ID,
						"automation_name":  a.DisplayName,
						"step_name":        step.StepName,
						"enabled":          a.Enabled,
						"last_executed_at": a.LastExecutedAt,
						"args":             argKeys,
					})
					emitted++
					if limitReached(enc.Written(), limit) {
						return nil
					}
				}
			}

			if resp.NextPageToken == "" || manualPaging {
				break
			}
			pageToken = resp.NextPageToken
		}

		if emitted == 0 {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "no automations call function %s\n", functionID)
		}
		return nil
	},
}

func init() {
	functionsUsageCmd.Flags().Int("page-size", 50, "Results per page (max 100)")
	functionsUsageCmd.Flags().String("page-token", "", "Pagination cursor")
	addLimitFlag(functionsUsageCmd)
	functionsCmd.AddCommand(functionsUsageCmd)
}
