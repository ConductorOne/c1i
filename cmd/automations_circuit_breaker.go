package cmd

import (
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var pausedDecisionFlag = enumFlag{flag: "decision", prefix: "PAUSED_EXECUTION_DECISION_", unknown: enumReject, values: []enumValue{
	{name: "run", api: "PAUSED_EXECUTION_DECISION_RUN"},
	{name: "cancel", api: "PAUSED_EXECUTION_DECISION_CANCEL"},
}}

var automationsCircuitBreakerCmd = &cobra.Command{
	Use:   "circuit-breaker",
	Short: "Clear an automation's circuit breaker or resolve its paused executions",
	Long: `An automation's circuit breaker (circuitBreakerMax per circuitBreakerPeriod,
set through "automations update --body-file") disables it when it runs too
often; executions that arrive while it is tripped are paused. "automations
get" shows the limit but no field saying whether the breaker is tripped.`,
}

var automationsCircuitBreakerClearCmd = &cobra.Command{
	Use:   "clear <automation-id>",
	Short: "Clear an automation's circuit breaker (pretty JSON)",
	Long: `Clear the circuit breaker on an automation it disabled. Without --decision only
the breaker is cleared and executions already paused stay paused (resolve them
with "automations circuit-breaker resolve-paused"); --decision run or cancel
also resolves them in the same request.

Prints the API's response as pretty JSON, {"bulkActionId": "..."}. Without
--decision the id is empty; with one, an id came back even when nothing was
paused. The API describes the resolution as asynchronous: exit 0 means
accepted, not finished. c1i has no command to read a bulk action. Honors
--dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := pausedDecisionBody(cmd)
		if err != nil {
			return err
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: client.Path("/api/v1/automations/%s/circuit_breaker/clear", args[0]), Body: body})
	},
}

var automationsCircuitBreakerResolvePausedCmd = &cobra.Command{
	Use:   "resolve-paused <automation-id>",
	Short: "Run or cancel an automation's executions paused by its circuit breaker (pretty JSON)",
	Long: `Run or cancel the executions paused while the automation's circuit breaker was
tripped. --decision is required.

Prints the API's response, {"bulkActionId": "..."} (it may also carry the
deprecated pausedCount and erroredCount). The API describes the resolution
as asynchronous: exit 0 means accepted, not finished. c1i has no command to
read a bulk action. Honors --dry-run.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireNonEmpty(cmd, "decision"); err != nil {
			return &usageError{err}
		}
		body, err := pausedDecisionBody(cmd)
		if err != nil {
			return err
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: client.Path("/api/v1/automations/%s/circuit_breaker/resolve_paused", args[0]), Body: body})
	},
}

// pausedDecisionBody builds the {decision, reason} body both circuit-breaker
// verbs send; an omitted flag is not sent.
func pausedDecisionBody(cmd *cobra.Command) (map[string]any, error) {
	body := map[string]any{}
	decision, err := pausedDecisionFlag.parse(mustGetString(cmd, "decision"))
	if err != nil && flagGiven(cmd, "decision") {
		return nil, err
	}
	if decision != "" {
		body["decision"] = decision
	}
	if reason := mustGetString(cmd, "reason"); reason != "" {
		body["reason"] = reason
	}
	return body, nil
}

func init() {
	clear := automationsCircuitBreakerClearCmd.Flags()
	clear.String("decision", "", "Also resolve paused executions: "+pausedDecisionFlag.usage()+" (omit to clear the breaker only)")
	clear.String("reason", "", "Reason recorded with the decision")
	resolve := automationsCircuitBreakerResolvePausedCmd.Flags()
	resolve.String("decision", "", "What to do with the paused executions: "+pausedDecisionFlag.usage())
	resolve.String("reason", "", "Reason recorded with the decision")
	annotateRequired(automationsCircuitBreakerResolvePausedCmd, "decision")

	for _, c := range []*cobra.Command{automationsCircuitBreakerClearCmd, automationsCircuitBreakerResolvePausedCmd} {
		markMutating(c)
		automationsCircuitBreakerCmd.AddCommand(c)
	}
	automationsCmd.AddCommand(automationsCircuitBreakerCmd)
}
