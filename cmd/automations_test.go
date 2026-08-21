package cmd

import (
	"reflect"
	"testing"
)

// TestMapExecutionState pins the user-friendly state names to the bare enum
// values the C1 API expects. The full AUTOMATION_EXECUTION_STATE_* names are
// verbose; the short forms (done|error|pending) are what humans actually
// remember. Case-insensitivity is part of the contract.
func TestMapExecutionState(t *testing.T) {
	cases := map[string]string{
		"":           "",
		"done":       "AUTOMATION_EXECUTION_STATE_DONE",
		"DONE":       "AUTOMATION_EXECUTION_STATE_DONE",
		"success":    "AUTOMATION_EXECUTION_STATE_DONE",
		"error":      "AUTOMATION_EXECUTION_STATE_ERROR",
		"Error":      "AUTOMATION_EXECUTION_STATE_ERROR",
		"failed":     "AUTOMATION_EXECUTION_STATE_ERROR",
		"failure":    "AUTOMATION_EXECUTION_STATE_ERROR",
		"pending":    "AUTOMATION_EXECUTION_STATE_PENDING",
		"creating":   "AUTOMATION_EXECUTION_STATE_CREATING",
		"waiting":    "AUTOMATION_EXECUTION_STATE_WAITING",
		"terminate":  "AUTOMATION_EXECUTION_STATE_TERMINATE",
		"terminated": "AUTOMATION_EXECUTION_STATE_TERMINATE",
		// Unknown values pass through so a future enum addition surfaces as
		// a server-side validation error rather than a silent client drop.
		"AUTOMATION_EXECUTION_STATE_PROCESS_STEP": "AUTOMATION_EXECUTION_STATE_PROCESS_STEP",
		"unknown": "unknown",
	}
	for in, want := range cases {
		if got := mapExecutionState(in); got != want {
			t.Errorf("mapExecutionState(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestStepCallsFunction covers the --calls-function predicate used in
// `automations list`. The fixture mirrors a real automation with three
// steps, only one of which is a callFunction.
func TestStepCallsFunction(t *testing.T) {
	steps := []automationStep{
		{StepName: "createAccessReview", CallFunction: nil},
		{StepName: "callFn1", CallFunction: &struct {
			FunctionID string `json:"functionId"`
		}{FunctionID: "fn-a"}},
		{StepName: "callFn2", CallFunction: &struct {
			FunctionID string `json:"functionId"`
		}{FunctionID: "fn-b"}},
	}
	if !stepCallsFunction(steps, "fn-a") {
		t.Errorf("expected match for fn-a")
	}
	if !stepCallsFunction(steps, "fn-b") {
		t.Errorf("expected match for fn-b")
	}
	if stepCallsFunction(steps, "fn-c") {
		t.Errorf("expected no match for fn-c")
	}
	if stepCallsFunction(nil, "anything") {
		t.Errorf("expected no match against nil steps")
	}
}

// TestUniqueFunctionIDs dedupes and preserves order. We surface function_ids
// on every list row, so the order needs to be stable across runs even when
// the same function is invoked twice in different steps.
func TestUniqueFunctionIDs(t *testing.T) {
	steps := []automationStep{
		{StepName: "noop", CallFunction: nil},
		{StepName: "a1", CallFunction: &struct {
			FunctionID string `json:"functionId"`
		}{FunctionID: "fn-a"}},
		{StepName: "b1", CallFunction: &struct {
			FunctionID string `json:"functionId"`
		}{FunctionID: "fn-b"}},
		{StepName: "a2", CallFunction: &struct {
			FunctionID string `json:"functionId"`
		}{FunctionID: "fn-a"}},
		{StepName: "blank", CallFunction: &struct {
			FunctionID string `json:"functionId"`
		}{FunctionID: ""}},
	}
	got := uniqueFunctionIDs(steps)
	want := []string{"fn-a", "fn-b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("uniqueFunctionIDs = %v, want %v", got, want)
	}

	// nil input returns nil (not empty slice) — callers serialize this to
	// JSON where the distinction shows up. The list output currently emits
	// "function_ids": null for automations with no function steps; pin
	// that behavior.
	if uniqueFunctionIDs(nil) != nil {
		t.Errorf("uniqueFunctionIDs(nil) should be nil")
	}
}

// TestAutomationRowLastExecutedAtIsNullNotEmptyString pins that
// last_executed_at is untyped nil, not "", for an automation that has never
// run — "" is truthy in jq and would make `jq 'select(.last_executed_at)'`
// match every row.
func TestAutomationRowLastExecutedAtIsNullNotEmptyString(t *testing.T) {
	tests := []struct {
		name           string
		lastExecutedAt string
		want           any
	}{
		{name: "never executed", lastExecutedAt: "", want: nil},
		{name: "executed", lastExecutedAt: "2026-01-02T03:04:05Z", want: "2026-01-02T03:04:05Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := automationRow(automationListItem{ID: "a1", LastExecutedAt: tt.lastExecutedAt})
			got, ok := row["last_executed_at"]
			if !ok {
				t.Fatal("row has no last_executed_at key")
			}
			if tt.want == nil {
				if got != nil {
					t.Fatalf("last_executed_at = %#v, want untyped nil", got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("last_executed_at = %v, want %v", got, tt.want)
			}
		})
	}
}
