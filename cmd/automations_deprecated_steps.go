package cmd

import (
	"errors"
	"fmt"
)

// deprecatedAutomationStepArm is one AutomationStep oneof arm the server no
// longer accepts, with its replacement arm ("" for none).
type deprecatedAutomationStepArm struct {
	arm, replacement string
}

// deprecatedAutomationStepArms lists every step oneof arm the server
// currently refuses outright: evaluateExpressions with a plain 400 ("the
// evaluate expressions step is no longer supported"), createRevokeTasks the
// same way (use createRevokeTasksV2). Checked in this order so a step
// setting both names evaluateExpressions first.
var deprecatedAutomationStepArms = []deprecatedAutomationStepArm{
	{"evaluateExpressions", ""},
	{"createRevokeTasks", "createRevokeTasksV2"},
}

// automationStepArrayFields are the body keys, either spelling, holding an
// array of AutomationStep objects: the published steps and the draft's own
// copy.
var automationStepArrayFields = []string{"automationSteps", "draftAutomationSteps"}

// refuseDeprecatedAutomationSteps refuses a step using evaluateExpressions or
// createRevokeTasks in automationSteps or draftAutomationSteps, in either
// body's spelling, before any request is sent (a --dry-run preview
// included: the caller runs this before sendMutation unconditionally).
// A non-array or non-object element is left to readJSONFile's own shape
// check or the server; this only rejects a step this specific.
func refuseDeprecatedAutomationSteps(body map[string]any) error {
	for _, field := range automationStepArrayFields {
		raw, key, err := bodyField(body, field)
		if err != nil {
			return err
		}
		if key == "" {
			continue
		}
		steps, ok := raw.([]any)
		if !ok {
			continue
		}
		for i, s := range steps {
			step, ok := s.(map[string]any)
			if !ok {
				continue
			}
			for _, d := range deprecatedAutomationStepArms {
				v, stepKey, err := bodyField(step, d.arm)
				if err != nil {
					return &usageError{fmt.Errorf("%s[%d]: %w", key, i, err)}
				}
				// null is the API's "unset", not a use of the arm: protojson
				// skips a null oneof member, so the server never sees it.
				if stepKey == "" || v == nil {
					continue
				}
				msg := fmt.Sprintf("%s[%d].%s is no longer accepted by the server", key, i, stepKey)
				if d.replacement != "" {
					msg += " (use " + d.replacement + ")"
				}
				return &usageError{errors.New(msg)}
			}
		}
	}
	return nil
}
