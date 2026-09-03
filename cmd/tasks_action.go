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
// action commands differ only in these fields, so they share one RunE: five
// near-identical copies existed before this.
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

	// The URL is resolved even for a preview: --dry-run answers "am I about to
	// do this to the right tenant", so it must still reject a bad --url and
	// still warn when the target came from config rather than the flag.
	baseURL, err := GetBaseURL()
	if err != nil {
		return err
	}

	// Credentials, though, are only needed to send. An action that takes no
	// policy step can preview without them; resolving a step needs a GET, so
	// those must authenticate first — which is what each command did before
	// sharing this runner.
	if a.step == stepUnused && dryRunActive() {
		return printDryRun(cmd, "POST", path, body)
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
