package cmd

import (
	"fmt"

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

// taskAction describes one POST /api/v1/tasks/{id}/action/{verb} command. The
// eleven action commands differ only in these fields, so they share one RunE:
// hand-rolling each was already six near-identical copies before this.
type taskAction struct {
	verb string // the path segment, e.g. "restart"
	step policyStepMode
	// extraBody adds fields beyond comment/policyStepId. It runs before the
	// request is built, so it may also reject bad flag combinations.
	extraBody func(cmd *cobra.Command, body map[string]any) error
	// confirm formats the success line. State is passed but most actions must
	// not print it — see runTaskAction.
	confirm func(id, state, stepID string) string
}

// runTaskAction is the shared RunE. Ordering matters and matches the rest of
// the repo: flags are validated before a client is built, so a usage error
// exits 2 rather than failing on credentials first.
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
	path := client.Path("/api/v1/tasks/%s/action/%s", taskID, a.verb)
	if comment != "" {
		body["comment"] = comment
	}

	// An action that needs no policy step needs no client to preview, so
	// --dry-run works without credentials. Resolving a step requires a GET, so
	// those actions must authenticate first even for a preview — which is what
	// each command did before sharing this runner.
	if a.step == stepUnused {
		if dryRunActive() {
			return printDryRun(cmd, "POST", path, body)
		}
	}

	baseURL, err := GetBaseURL()
	if err != nil {
		return err
	}
	c, err := newClient(cmd, baseURL)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	var stepID string
	if a.step != stepUnused {
		explicit, _ := cmd.Flags().GetString("policy-step-id")
		stepID, err = resolvePolicyStepID(cmd.Context(), c, taskID, explicit, a.step == stepRequired)
		if err != nil {
			return err
		}
		if stepID != "" {
			body["policyStepId"] = stepID
		}
		if dryRunActive() {
			return printDryRun(cmd, "POST", path, body)
		}
	}

	data, err := c.Post(cmd.Context(), path, body)
	if err != nil {
		return fmt.Errorf("API error: %w", err)
	}
	id, state, err := parseTaskActionResponse(data)
	if err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s", a.confirm(id, state, stepID))
	return nil
}

// addTaskActionFlags registers the flags the shared RunE reads. Only --comment
// is universal; --policy-step-id is registered when the action uses one.
func addTaskActionFlags(cmd *cobra.Command, step policyStepMode, stepUsage string) {
	cmd.Flags().String("comment", "", "Optional comment")
	if step != stepUnused {
		cmd.Flags().String("policy-step-id", "", stepUsage)
	}
}
