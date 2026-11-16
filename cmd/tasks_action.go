package cmd

import (
	"context"
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// policyStepMode says whether an action's request needs a policy step id.
type policyStepMode int

const (
	stepUnused   policyStepMode = iota // the endpoint takes no policyStepId
	stepOptional                       // send it when it can be resolved, omit otherwise
	stepRequired                       // the server rejects the call without it
)

// taskAction describes one POST /api/v1/tasks/{id}/action/{verb} command.
// These fields are all the commands differ by, so they share one RunE.
type taskAction struct {
	verb string // the path segment, e.g. "restart"
	step policyStepMode
	// extraBody adds fields beyond comment/policyStepId, and may reject bad
	// flag combinations. Runs before any client is built, so it exits 2.
	extraBody func(cmd *cobra.Command, body map[string]any) error
	// preReads adds pre-send reads beyond the step lookup, e.g. checking that
	// a flag names a real user. Runs before the send, on a dry run too unless
	// the returned preRead says otherwise.
	preReads func(cmd *cobra.Command) []preRead
}

// runTaskAction is the shared RunE. Flags are validated before a client is
// built, so a usage error exits 2 rather than failing on credentials. The
// response's .state is the task before the action, not the outcome.
func (a taskAction) runTaskAction(cmd *cobra.Command, args []string) error {
	var comment string
	if cmd.Flags().Lookup("comment") != nil {
		comment, _ = cmd.Flags().GetString("comment")
	}

	body := map[string]any{}
	if a.extraBody != nil {
		if err := a.extraBody(cmd, body); err != nil {
			return err
		}
	}

	taskID := args[0]
	if comment != "" {
		body["comment"] = comment
	}
	m := mutation{Method: http.MethodPost, Path: client.Path("/api/v1/tasks/%s/action/%s", taskID, a.verb), Body: body}
	var opts []mutationOpt
	if a.step != stepUnused {
		// An explicit step needs no lookup, so a preview needs no credentials.
		if explicit, _ := cmd.Flags().GetString("policy-step-id"); explicit != "" {
			body["policyStepId"] = explicit
		} else {
			opts = append(opts, withPreRead(a.stepLookup(taskID)))
		}
	}
	if a.preReads != nil {
		for _, p := range a.preReads(cmd) {
			opts = append(opts, withPreRead(p))
		}
	}
	return sendMutation(cmd, m, opts...)
}

// stepLookup resolves the current policy step before the send, on a dry run
// too, and reports it on stderr.
func (a taskAction) stepLookup(taskID string) preRead {
	return preRead{Run: func(ctx context.Context, c *client.Client, m *mutation) ([]resolvedID, error) {
		stepID, err := resolvePolicyStepID(ctx, c, taskID, "", a.step == stepRequired)
		if err != nil || stepID == "" {
			return nil, err
		}
		m.Body.(map[string]any)["policyStepId"] = stepID
		return []resolvedID{{Name: "policyStepId", Label: "policy step", ID: stepID}}, nil
	}}
}
