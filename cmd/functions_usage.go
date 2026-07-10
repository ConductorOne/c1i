package cmd

import (
	"encoding/json"
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

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		enc := newEmitter(cmd.OutOrStdout())
		pageToken := ""
		matched := 0
		for {
			params := map[string]string{"page_size": "100"}
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
					matched++
				}
			}

			if resp.NextPageToken == "" {
				break
			}
			pageToken = resp.NextPageToken
		}

		if matched == 0 {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "no automations call function %s\n", functionID)
		}
		return nil
	},
}

func init() {
	functionsCmd.AddCommand(functionsUsageCmd)
}
