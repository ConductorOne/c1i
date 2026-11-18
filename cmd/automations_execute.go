package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

const (
	executionStateDone   = "AUTOMATION_EXECUTION_STATE_DONE"
	executionStateError  = "AUTOMATION_EXECUTION_STATE_ERROR"
	executionStateTerm   = "AUTOMATION_EXECUTION_STATE_TERMINATE"
	executionStatePaused = "AUTOMATION_EXECUTION_STATE_PAUSED_BY_CIRCUIT_BREAKER"
)

// executionWaitInterval is how often execute --wait re-reads. A var so a test
// can poll fast.
var executionWaitInterval = 2 * time.Second

var automationsExecuteCmd = &cobra.Command{
	Use:   "execute <automation-id>",
	Short: "Run an automation now (pretty JSON)",
	Long: `Start an on-demand execution of an automation. Prints the API's response as
pretty JSON, {"executionId": "..."}.
Read the execution with "automations executions get <execution-id>"; stop it
with "automations executions terminate <execution-id>".

--context-file is a JSON object sent as the execution's context (the request's
context.context), the values the automation's steps read.

--wait polls "automations executions get" until the execution ends. The
executionId is printed as soon as the execution is accepted; progress goes to
stderr. An execution that ends in AUTOMATION_EXECUTION_STATE_ERROR or
_TERMINATE, or is paused by the circuit breaker, exits 1 with an error naming
the state; the execution was started (applied: true). A timeout also exits 1,
and the execution may still be running. --dry-run never polls.

Example:
  EXEC_ID=$(c1i automations execute "$AUTOMATION" | jq -er .executionId)
  c1i automations execute "$AUTOMATION" --wait --wait-timeout 10m`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		wait, waitTimeout, err := waitFlagValues(cmd)
		if err != nil {
			return err
		}
		body := map[string]any{}
		if path := mustGetString(cmd, "context-file"); path != "" {
			ctxObj, err := readJSONFile[map[string]any](cmd, "context-file", path)
			if err != nil {
				return err
			}
			body["context"] = map[string]any{"context": ctxObj}
		}
		path := client.Path("/api/v1/automations/%s/execute", args[0])
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: path, Body: body},
			withResponse(func(c *client.Client, resp []byte) writeOutcome {
				var r struct {
					ExecutionID string `json:"executionId"`
				}
				if json.Unmarshal(resp, &r) != nil || r.ExecutionID == "" {
					return writeOutcome{Err: &nonJSONResponseError{fmt.Errorf("the execute response has no executionId; the automation was probably started, find the execution with: c1i automations executions list --template-id %s", args[0])}}
				}
				return writeOutcome{Wait: executionDoneWaitOp(c, r.ExecutionID, waitTimeout)}
			}, wait))
	},
}

// executionDoneWaitOp polls one execution until it is DONE. A failed end
// state stops the wait with an error naming it.
func executionDoneWaitOp(c *client.Client, id string, timeout time.Duration) waitOp[[]string] {
	return waitOp[[]string]{
		Poll: func(ctx context.Context) ([]string, error) {
			data, err := c.Get(ctx, client.Path("/api/v1/automation_executions/%s", id), nil)
			if err != nil {
				return nil, fmt.Errorf("reading execution %s: %w", id, err)
			}
			var r struct {
				AutomationExecution struct {
					State string `json:"state"`
				} `json:"automationExecution"`
			}
			if err := json.Unmarshal(data, &r); err != nil || r.AutomationExecution.State == "" {
				return nil, &nonJSONResponseError{fmt.Errorf("execution %s: the response has no automationExecution.state", id)}
			}
			switch s := r.AutomationExecution.State; s {
			case executionStateError, executionStateTerm:
				return nil, fmt.Errorf("execution %s was started and ended in state %s; inspect it with: c1i automations executions get %s", id, s, id)
			case executionStatePaused:
				return nil, fmt.Errorf("execution %s was started and is in state %s; run or cancel it with: c1i automations circuit-breaker resolve-paused <automation-id> --decision run|cancel", id, s)
			default:
				return []string{s}, nil
			}
		},
		Done:     func(s []string) bool { return len(s) == 1 && s[0] == executionStateDone },
		Interval: executionWaitInterval,
		Timeout:  timeout,
		Subject:  "execution " + id + " to finish",
		Success:  "Execution " + id + " finished (" + executionStateDone + ")",
		Slow:     "an automation's steps can run for minutes or longer",
		Recheck:  "c1i automations executions get " + id,
	}
}

func init() {
	f := automationsExecuteCmd.Flags()
	f.String("context-file", "", "JSON object sent as the execution's context (file, or \"-\" for stdin)")
	addWaitFlags(automationsExecuteCmd, "GET /api/v1/automation_executions/{id} until the execution ends", 4*time.Minute)
	markMutating(automationsExecuteCmd)
	automationsCmd.AddCommand(automationsExecuteCmd)
}
