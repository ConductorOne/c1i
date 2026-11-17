package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var functionsUsageCmd = &cobra.Command{
	Use:   "usage <function-id>",
	Short: "List automations that invoke this function (NDJSON output)",
	Long: `Scan automations in the tenant and emit one row per automation step
that calls this function. Useful for "is this function still used?"
audits before deleting a draft, or to find an example automation that
exercises a published function.

Reads POST /api/v1/search/automations and inspects automationSteps[].callFunction.
NDJSON fields: automation_id, automation_name, step_name, enabled,
last_executed_at, args.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		functionID := args[0]
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		// emitted counts the rows this command found, for the no-match notice below.
		emitted := 0
		if err := listPages(cmd, c, pageSpec[usageAutomation]{
			Route: "POST /api/v1/search/automations",
			Fetch: postPage(cmd.Context(), "/api/v1/search/automations", map[string]any{}),
			Parse: parseList[usageAutomation],
			ID:    func(a usageAutomation) string { return a.ID },
			// Page size tracks --page-size alone: rows are filtered client-side, so
			// fetched is not written and tightening toward --limit would amplify requests.
			FixedSize: true,
			Rows: func(a usageAutomation) []any {
				var rows []any
				for _, step := range a.AutomationSteps {
					if step.CallFunction == nil || step.CallFunction.FunctionID != functionID {
						continue
					}
					argKeys := make([]string, 0, len(step.CallFunction.Args))
					for k := range step.CallFunction.Args {
						argKeys = append(argKeys, k)
					}
					emitted++
					rows = append(rows, map[string]any{
						"automation_id":    nilIfEmpty(a.ID),
						"automation_name":  nilIfEmpty(a.DisplayName),
						"step_name":        nilIfEmpty(step.StepName),
						"enabled":          a.Enabled,
						"last_executed_at": nilIfEmpty(a.LastExecutedAt),
						"args":             argKeys,
					})
				}
				return rows
			},
		}); err != nil {
			return err
		}

		if emitted == 0 {
			if cmd.Flags().Changed("page-token") {
				notice(cmd, noticeInfo, fmt.Sprintf("no automations on this page call function %s (--page-token scans one page only)", functionID), nil)
			} else {
				notice(cmd, noticeInfo, fmt.Sprintf("no automations call function %s", functionID), nil)
			}
		}
		return nil
	},
}

// usageAutomation is the subset of an automation that functions usage reads.
type usageAutomation struct {
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
}

func init() {
	addPaginationFlags(functionsUsageCmd)
	functionsCmd.AddCommand(functionsUsageCmd)
}
