package cmd

import (
	"errors"
	"strings"
	"testing"
)

// requireUsageError fails the test unless err is a *usageError. A guard that
// returns a bare error instead would still fail the request, but silently
// exit 1 (generic failure) instead of 2 (usage) — the distinction every
// guard test in this file now pins, not just "err != nil".
func requireUsageError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	var usageErr *usageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("error is not a *usageError (would exit 1, not 2): %v (%T)", err, err)
	}
}

// ---- Guard 1: empty/missing policySteps ----

func TestValidatePolicyStepsNonEmpty_FiresOnMissing(t *testing.T) {
	err := validatePolicyStepsNonEmpty("POLICY_TYPE_GRANT", nil, false)
	requireUsageError(t, err)
	if !strings.Contains(err.Error(), "deny-everything") {
		t.Errorf("error should explain the deny-all default, got: %v", err)
	}
}

func TestValidatePolicyStepsNonEmpty_FiresOnEmptyStepsArray(t *testing.T) {
	steps := map[string]any{
		"grant": map[string]any{"steps": []any{}},
	}
	err := validatePolicyStepsNonEmpty("POLICY_TYPE_GRANT", steps, false)
	requireUsageError(t, err)
}

func TestValidatePolicyStepsNonEmpty_FiresOnWrongKey(t *testing.T) {
	// Steps supplied, but keyed by the enum constant instead of the
	// lowercase word the server actually looks up ("grant").
	steps := map[string]any{
		"POLICY_TYPE_GRANT": map[string]any{
			"steps": []any{map[string]any{"reject": map[string]any{}}},
		},
	}
	err := validatePolicyStepsNonEmpty("POLICY_TYPE_GRANT", steps, false)
	requireUsageError(t, err)
}

func TestValidatePolicyStepsNonEmpty_NotFiresOnRealSteps(t *testing.T) {
	steps := map[string]any{
		"grant": map[string]any{
			"steps": []any{map[string]any{"approval": map[string]any{"users": map[string]any{"userIds": []any{"u1"}}}}},
		},
	}
	if err := validatePolicyStepsNonEmpty("POLICY_TYPE_GRANT", steps, false); err != nil {
		t.Errorf("expected no error for valid non-empty steps, got: %v", err)
	}
}

func TestValidatePolicyStepsNonEmpty_AllowDenyAllBypassesMissing(t *testing.T) {
	if err := validatePolicyStepsNonEmpty("POLICY_TYPE_GRANT", nil, true); err != nil {
		t.Errorf("--allow-deny-all should bypass the missing-steps guard, got: %v", err)
	}
}

func TestValidatePolicyStepsNonEmpty_AllowDenyAllDoesNotBypassExplicitEmptyArray(t *testing.T) {
	// An explicit steps:[] crashes the server (bare error -> 500) regardless
	// of intent; --allow-deny-all must not be able to wave that through.
	steps := map[string]any{"grant": map[string]any{"steps": []any{}}}
	err := validatePolicyStepsNonEmpty("POLICY_TYPE_GRANT", steps, true)
	requireUsageError(t, err)
}

// ---- Guard 2: policyType required ----

func TestValidatePolicyType_FiresOnEmpty(t *testing.T) {
	requireUsageError(t, validatePolicyType(""))
}

func TestValidatePolicyType_FiresOnUnspecified(t *testing.T) {
	requireUsageError(t, validatePolicyType("POLICY_TYPE_UNSPECIFIED"))
}

func TestValidatePolicyType_NotFiresOnValidType(t *testing.T) {
	for _, pt := range []string{"POLICY_TYPE_GRANT", "POLICY_TYPE_REVOKE", "POLICY_TYPE_CERTIFY"} {
		if err := validatePolicyType(pt); err != nil {
			t.Errorf("expected no error for %s, got: %v", pt, err)
		}
	}
}

// ---- Guard 3: empty rules[].condition ----

func TestValidateRuleConditions_FiresOnEmpty(t *testing.T) {
	rules := []any{map[string]any{"condition": "", "stepKey": "grant"}}
	requireUsageError(t, validateRuleConditions(rules))
}

func TestValidateRuleConditions_FiresOnWhitespaceOnly(t *testing.T) {
	rules := []any{map[string]any{"condition": "   ", "stepKey": "grant"}}
	requireUsageError(t, validateRuleConditions(rules))
}

func TestValidateRuleConditions_NotFiresOnLiteralTrue(t *testing.T) {
	rules := []any{map[string]any{"condition": "true", "stepKey": "grant"}}
	if err := validateRuleConditions(rules); err != nil {
		t.Errorf("expected no error for a baseline literal \"true\" condition, got: %v", err)
	}
}

func TestValidateRuleConditions_NotFiresOnRealCondition(t *testing.T) {
	rules := []any{map[string]any{"condition": `subject.role == "admin"`, "stepKey": "grant"}}
	if err := validateRuleConditions(rules); err != nil {
		t.Errorf("expected no error for a real condition, got: %v", err)
	}
}

// ---- Rule outcome oneof (stepKey xor policyKey) ----

func TestValidateRuleOutcomes_FiresOnNeither(t *testing.T) {
	rules := []any{map[string]any{"condition": "true"}}
	requireUsageError(t, validateRuleOutcomes(rules))
}

func TestValidateRuleOutcomes_FiresOnBoth(t *testing.T) {
	rules := []any{map[string]any{"condition": "true", "stepKey": "grant", "policyKey": "grant"}}
	requireUsageError(t, validateRuleOutcomes(rules))
}

func TestValidateRuleOutcomes_NotFiresOnStepKeyOnly(t *testing.T) {
	rules := []any{map[string]any{"condition": "true", "stepKey": "grant"}}
	if err := validateRuleOutcomes(rules); err != nil {
		t.Errorf("expected no error for a rule with only stepKey set, got: %v", err)
	}
}

func TestValidateRuleOutcomes_NotFiresOnDeprecatedPolicyKeyOnly(t *testing.T) {
	rules := []any{map[string]any{"condition": "true", "policyKey": "grant"}}
	if err := validateRuleOutcomes(rules); err != nil {
		t.Errorf("expected no error for a rule with only the deprecated policyKey set, got: %v", err)
	}
}

// ---- rules[].stepKey must reference a real, non-empty policySteps entry ----

func TestValidateRuleStepKeys_FiresOnMismatchedKey(t *testing.T) {
	rules := []any{map[string]any{"condition": "true", "stepKey": "nonexistent"}}
	policySteps := map[string]any{"grant": map[string]any{"steps": []any{map[string]any{"reject": map[string]any{}}}}}
	requireUsageError(t, validateRuleStepKeys(rules, policySteps))
}

func TestValidateRuleStepKeys_FiresOnKeyWithEmptySteps(t *testing.T) {
	rules := []any{map[string]any{"condition": "true", "stepKey": "escalated"}}
	policySteps := map[string]any{
		"grant":     map[string]any{"steps": []any{map[string]any{"reject": map[string]any{}}}},
		"escalated": map[string]any{"steps": []any{}},
	}
	requireUsageError(t, validateRuleStepKeys(rules, policySteps))
}

func TestValidateRuleStepKeys_NotFiresOnMatchingKey(t *testing.T) {
	rules := []any{map[string]any{"condition": "true", "stepKey": "escalated"}}
	policySteps := map[string]any{
		"grant":     map[string]any{"steps": []any{map[string]any{"reject": map[string]any{}}}},
		"escalated": map[string]any{"steps": []any{map[string]any{"approval": map[string]any{"users": map[string]any{"userIds": []any{"u1"}}}}}},
	}
	if err := validateRuleStepKeys(rules, policySteps); err != nil {
		t.Errorf("expected no error for a rule whose stepKey matches a real entry, got: %v", err)
	}
}

func TestValidateRuleStepKeys_NotFiresOnPolicyKeyOrPolicyIdOutcome(t *testing.T) {
	// Neither the deprecated policyKey nor a policyId outcome is checked
	// against policySteps here (policyId routes to a DIFFERENT policy
	// entirely; policyKey is legacy round-trip only).
	rules := []any{
		map[string]any{"condition": "true", "policyKey": "whatever"},
		map[string]any{"condition": "true", "policyId": "some-other-policy"},
	}
	policySteps := map[string]any{"grant": map[string]any{"steps": []any{map[string]any{"reject": map[string]any{}}}}}
	if err := validateRuleStepKeys(rules, policySteps); err != nil {
		t.Errorf("expected no error for policyKey/policyId outcomes, got: %v", err)
	}
}

func TestValidateRuleStepKeys_NotFiresWhenPolicyStepsNotSupplied(t *testing.T) {
	// On update, rules[] can be changed without policySteps also being
	// part of the same patch; there's nothing to check against.
	rules := []any{map[string]any{"condition": "true", "stepKey": "grant"}}
	if err := validateRuleStepKeys(rules, nil); err != nil {
		t.Errorf("expected no error when policySteps isn't part of this request, got: %v", err)
	}
}

// ---- Guard 4: fallback/fallbackUserIds on the wrong arm ----

func approvalStep(arm string, armBody map[string]any) map[string]any {
	return map[string]any{
		"grant": map[string]any{
			"steps": []any{
				map[string]any{"approval": map[string]any{arm: armBody}},
			},
		},
	}
}

func TestValidateApprovalFallback_FiresOnUsersArmWithFallback(t *testing.T) {
	steps := approvalStep("users", map[string]any{"userIds": []any{"u1"}, "fallback": true})
	requireUsageError(t, validateApprovalFallback(steps, "POLICY_TYPE_GRANT"))
}

func TestValidateApprovalFallback_FiresOnAppOwnersArmWithFallbackUserIds(t *testing.T) {
	steps := approvalStep("appOwners", map[string]any{"fallbackUserIds": []any{"u1"}})
	requireUsageError(t, validateApprovalFallback(steps, "POLICY_TYPE_GRANT"))
}

func TestValidateApprovalFallback_FiresOnWebhookArmWithFallback(t *testing.T) {
	steps := approvalStep("webhook", map[string]any{"webhookId": "wh1", "fallback": true})
	requireUsageError(t, validateApprovalFallback(steps, "POLICY_TYPE_GRANT"))
}

func TestValidateApprovalFallback_NotFiresOnUsersArmWithoutFallback(t *testing.T) {
	steps := approvalStep("users", map[string]any{"userIds": []any{"u1"}})
	if err := validateApprovalFallback(steps, "POLICY_TYPE_GRANT"); err != nil {
		t.Errorf("expected no error for a plain users arm, got: %v", err)
	}
}

// ---- Guard 5: fallback:true with nothing to fall back to (500 trap) ----

func TestValidateApprovalFallback_FiresOnManagerFallbackTrueEmptyUserIds(t *testing.T) {
	steps := approvalStep("manager", map[string]any{"fallback": true, "fallbackUserIds": []any{}})
	requireUsageError(t, validateApprovalFallback(steps, "POLICY_TYPE_GRANT"))
}

func TestValidateApprovalFallback_FiresOnGroupFallbackTrueGroupEnabledEmptyGroupIds(t *testing.T) {
	steps := approvalStep("group", map[string]any{
		"appGroupId": "g1", "appId": "a1",
		"fallback": true, "isGroupFallbackEnabled": true, "fallbackGroupIds": []any{},
	})
	requireUsageError(t, validateApprovalFallback(steps, "POLICY_TYPE_GRANT"))
}

func TestValidateApprovalFallback_NotFiresOnManagerFallbackTrueWithUserIds(t *testing.T) {
	steps := approvalStep("manager", map[string]any{"fallback": true, "fallbackUserIds": []any{"u1"}})
	if err := validateApprovalFallback(steps, "POLICY_TYPE_GRANT"); err != nil {
		t.Errorf("expected no error: manager fallback:true with real fallbackUserIds, got: %v", err)
	}
}

func TestValidateApprovalFallback_NotFiresOnFallbackFalse(t *testing.T) {
	steps := approvalStep("self", map[string]any{"fallback": false})
	if err := validateApprovalFallback(steps, "POLICY_TYPE_GRANT"); err != nil {
		t.Errorf("expected no error: fallback:false needs nothing, got: %v", err)
	}
}

func TestValidateApprovalFallback_NotFiresOnGroupFallbackTrueGroupIdsWithoutEnabledFlag(t *testing.T) {
	// isGroupFallbackEnabled is false/absent, so fallbackUserIds (not
	// fallbackGroupIds) is what must be checked.
	steps := approvalStep("group", map[string]any{
		"appGroupId": "g1", "appId": "a1",
		"fallback": true, "fallbackUserIds": []any{"u1"},
	})
	if err := validateApprovalFallback(steps, "POLICY_TYPE_GRANT"); err != nil {
		t.Errorf("expected no error: group fallback via fallbackUserIds (default path), got: %v", err)
	}
}

// ---- Additional bare-error/500 traps: provision step, assigned, manager.assignedUserIds, agent.agentUserId ----

func TestValidateApprovalFallback_FiresOnProvisionStep(t *testing.T) {
	steps := map[string]any{
		"grant": map[string]any{
			"steps": []any{map[string]any{"provision": map[string]any{}}},
		},
	}
	requireUsageError(t, validateApprovalFallback(steps, "POLICY_TYPE_GRANT"))
}

func TestValidateApprovalFallback_FiresOnApprovalAssignedTrue(t *testing.T) {
	steps := map[string]any{
		"grant": map[string]any{
			"steps": []any{map[string]any{"approval": map[string]any{
				"assigned": true,
				"users":    map[string]any{"userIds": []any{"u1"}},
			}}},
		},
	}
	requireUsageError(t, validateApprovalFallback(steps, "POLICY_TYPE_GRANT"))
}

func TestValidateApprovalFallback_FiresOnManagerAssignedUserIds(t *testing.T) {
	steps := approvalStep("manager", map[string]any{"assignedUserIds": []any{"u1"}})
	requireUsageError(t, validateApprovalFallback(steps, "POLICY_TYPE_GRANT"))
}

func TestValidateApprovalFallback_FiresOnAgentUserID(t *testing.T) {
	steps := approvalStep("agent", map[string]any{"agentUserId": "u1", "agentMode": "APPROVAL_AGENT_MODE_FULL_CONTROL"})
	requireUsageError(t, validateApprovalFallback(steps, "POLICY_TYPE_GRANT"))
}

func TestValidateApprovalFallback_NotFiresOnCleanAgentArm(t *testing.T) {
	steps := approvalStep("agent", map[string]any{
		"agentMode":          "APPROVAL_AGENT_MODE_FULL_CONTROL",
		"agentFailureAction": "APPROVAL_AGENT_FAILURE_ACTION_SKIP_POLICY_STEP",
	})
	if err := validateApprovalFallback(steps, "POLICY_TYPE_GRANT"); err != nil {
		t.Errorf("expected no error for a clean agent arm, got: %v", err)
	}
}

func TestValidateApprovalFallback_NotFiresOnCleanApprovalStep(t *testing.T) {
	steps := map[string]any{
		"grant": map[string]any{
			"steps": []any{map[string]any{"approval": map[string]any{
				"assigned": false,
				"manager":  map[string]any{"fallback": false},
			}}},
		},
	}
	if err := validateApprovalFallback(steps, "POLICY_TYPE_GRANT"); err != nil {
		t.Errorf("expected no error for a clean approval step, got: %v", err)
	}
}

// ---- Step oneof: a step with no recognized arm reaches the server as a nil step ----

func TestValidateApprovalFallback_FiresOnStepWithNoArm(t *testing.T) {
	steps := map[string]any{
		"grant": map[string]any{
			"steps": []any{map[string]any{}},
		},
	}
	err := validateApprovalFallback(steps, "POLICY_TYPE_GRANT")
	requireUsageError(t, err)
	if !strings.Contains(err.Error(), "policy step is nil") {
		t.Errorf("error should quote the server's \"policy step is nil\" text, got: %v", err)
	}
}

func TestValidateApprovalFallback_NotFiresOnEmptyRejectArmBody(t *testing.T) {
	// The arm is present with an empty body — a legitimate deny-all step,
	// distinct from a step with no arm set at all.
	steps := map[string]any{
		"grant": map[string]any{
			"steps": []any{map[string]any{"reject": map[string]any{}}},
		},
	}
	if err := validateApprovalFallback(steps, "POLICY_TYPE_GRANT"); err != nil {
		t.Errorf("expected no error: {\"reject\":{}} is a legitimate step, got: %v", err)
	}
}

func TestValidateApprovalFallback_NotFiresOnWaitArm(t *testing.T) {
	steps := map[string]any{
		"grant": map[string]any{
			"steps": []any{map[string]any{"wait": map[string]any{}}},
		},
	}
	if err := validateApprovalFallback(steps, "POLICY_TYPE_GRANT"); err != nil {
		t.Errorf("expected no error: \"wait\" is a recognized step arm, got: %v", err)
	}
}

// ---- Agent rule: agentMode is required ----

func TestValidateApprovalFallback_FiresOnAgentModeUnspecified(t *testing.T) {
	steps := approvalStep("agent", map[string]any{
		"agentMode":          "APPROVAL_AGENT_MODE_UNSPECIFIED",
		"agentFailureAction": "APPROVAL_AGENT_FAILURE_ACTION_SKIP_POLICY_STEP",
	})
	err := validateApprovalFallback(steps, "POLICY_TYPE_GRANT")
	requireUsageError(t, err)
	if !strings.Contains(err.Error(), "agent mode is required") {
		t.Errorf("error should quote the server's \"agent mode is required\" text, got: %v", err)
	}
}

func TestValidateApprovalFallback_NotFiresOnAgentModeSet(t *testing.T) {
	steps := approvalStep("agent", map[string]any{
		"agentMode":          "APPROVAL_AGENT_MODE_FULL_CONTROL",
		"agentFailureAction": "APPROVAL_AGENT_FAILURE_ACTION_SKIP_POLICY_STEP",
	})
	if err := validateApprovalFallback(steps, "POLICY_TYPE_GRANT"); err != nil {
		t.Errorf("expected no error: agentMode is set, got: %v", err)
	}
}

// ---- Agent rule: agent steps only allowed in grant/certify policies ----

func TestValidateApprovalFallback_FiresOnAgentInRevokePolicy(t *testing.T) {
	steps := approvalStep("agent", map[string]any{
		"agentMode":          "APPROVAL_AGENT_MODE_FULL_CONTROL",
		"agentFailureAction": "APPROVAL_AGENT_FAILURE_ACTION_SKIP_POLICY_STEP",
	})
	err := validateApprovalFallback(steps, "POLICY_TYPE_REVOKE")
	requireUsageError(t, err)
	if !strings.Contains(err.Error(), "agent approval steps are only allowed in grant and certify policies") {
		t.Errorf("error should quote the server's own text, got: %v", err)
	}
}

func TestValidateApprovalFallback_NotFiresOnAgentInGrantPolicy(t *testing.T) {
	steps := approvalStep("agent", map[string]any{
		"agentMode":          "APPROVAL_AGENT_MODE_FULL_CONTROL",
		"agentFailureAction": "APPROVAL_AGENT_FAILURE_ACTION_SKIP_POLICY_STEP",
	})
	if err := validateApprovalFallback(steps, "POLICY_TYPE_GRANT"); err != nil {
		t.Errorf("expected no error: agent steps are allowed in a grant policy, got: %v", err)
	}
}

func TestValidateApprovalFallback_AgentPolicyTypeRuleSkippedWhenTypeUnknown(t *testing.T) {
	// policyType == "" means the caller couldn't resolve it (see the doc
	// comment); guessing here would be worse than not checking.
	steps := approvalStep("agent", map[string]any{
		"agentMode":          "APPROVAL_AGENT_MODE_FULL_CONTROL",
		"agentFailureAction": "APPROVAL_AGENT_FAILURE_ACTION_SKIP_POLICY_STEP",
	})
	if err := validateApprovalFallback(steps, ""); err != nil {
		t.Errorf("expected no error when policyType is unknown, got: %v", err)
	}
}

// ---- Agent rule: certify policies require agentMode COMMENT_ONLY ----

func TestValidateApprovalFallback_FiresOnCertifyWithNonCommentOnlyMode(t *testing.T) {
	steps := approvalStep("agent", map[string]any{
		"agentMode":          "APPROVAL_AGENT_MODE_FULL_CONTROL",
		"agentFailureAction": "APPROVAL_AGENT_FAILURE_ACTION_SKIP_POLICY_STEP",
	})
	err := validateApprovalFallback(steps, "POLICY_TYPE_CERTIFY")
	requireUsageError(t, err)
	if !strings.Contains(err.Error(), `agent mode can only be "Comment only" in certify policies`) {
		t.Errorf("error should quote the server's own text, got: %v", err)
	}
}

func TestValidateApprovalFallback_NotFiresOnCertifyWithCommentOnlyMode(t *testing.T) {
	steps := approvalStep("agent", map[string]any{
		"agentMode":          "APPROVAL_AGENT_MODE_COMMENT_ONLY",
		"agentFailureAction": "APPROVAL_AGENT_FAILURE_ACTION_SKIP_POLICY_STEP",
	})
	if err := validateApprovalFallback(steps, "POLICY_TYPE_CERTIFY"); err != nil {
		t.Errorf("expected no error: COMMENT_ONLY is allowed in a certify policy, got: %v", err)
	}
}

// ---- Agent rule: CHANGE_POLICY_ONLY mode requires non-empty policyIds ----

func TestValidateApprovalFallback_FiresOnChangePolicyOnlyWithNoPolicyIds(t *testing.T) {
	steps := approvalStep("agent", map[string]any{
		"agentMode":          "APPROVAL_AGENT_MODE_CHANGE_POLICY_ONLY",
		"agentFailureAction": "APPROVAL_AGENT_FAILURE_ACTION_SKIP_POLICY_STEP",
	})
	err := validateApprovalFallback(steps, "POLICY_TYPE_GRANT")
	requireUsageError(t, err)
	if !strings.Contains(err.Error(), "policy ids are required when agent mode is") {
		t.Errorf("error should quote the server's own text, got: %v", err)
	}
}

func TestValidateApprovalFallback_NotFiresOnChangePolicyOnlyWithPolicyIds(t *testing.T) {
	steps := approvalStep("agent", map[string]any{
		"agentMode":          "APPROVAL_AGENT_MODE_CHANGE_POLICY_ONLY",
		"agentFailureAction": "APPROVAL_AGENT_FAILURE_ACTION_SKIP_POLICY_STEP",
		"policyIds":          []any{"other-policy-id"},
	})
	if err := validateApprovalFallback(steps, "POLICY_TYPE_GRANT"); err != nil {
		t.Errorf("expected no error: policyIds is non-empty, got: %v", err)
	}
}

// ---- Agent rule: agentFailureAction is required ----

func TestValidateApprovalFallback_FiresOnAgentFailureActionUnspecified(t *testing.T) {
	steps := approvalStep("agent", map[string]any{
		"agentMode":          "APPROVAL_AGENT_MODE_FULL_CONTROL",
		"agentFailureAction": "APPROVAL_AGENT_FAILURE_ACTION_UNSPECIFIED",
	})
	err := validateApprovalFallback(steps, "POLICY_TYPE_GRANT")
	requireUsageError(t, err)
	if !strings.Contains(err.Error(), "agent failure action is required") {
		t.Errorf("error should quote the server's own text, got: %v", err)
	}
}

func TestValidateApprovalFallback_NotFiresOnAgentFailureActionSet(t *testing.T) {
	steps := approvalStep("agent", map[string]any{
		"agentMode":          "APPROVAL_AGENT_MODE_FULL_CONTROL",
		"agentFailureAction": "APPROVAL_AGENT_FAILURE_ACTION_SKIP_POLICY_STEP",
	})
	if err := validateApprovalFallback(steps, "POLICY_TYPE_GRANT"); err != nil {
		t.Errorf("expected no error: agentFailureAction is set, got: %v", err)
	}
}

// ---- Agent rule: REASSIGN_TO_USERS requires non-empty reassignToUserIds ----

func TestValidateApprovalFallback_FiresOnReassignToUsersWithNoUserIds(t *testing.T) {
	steps := approvalStep("agent", map[string]any{
		"agentMode":          "APPROVAL_AGENT_MODE_FULL_CONTROL",
		"agentFailureAction": "APPROVAL_AGENT_FAILURE_ACTION_REASSIGN_TO_USERS",
	})
	err := validateApprovalFallback(steps, "POLICY_TYPE_GRANT")
	requireUsageError(t, err)
	if !strings.Contains(err.Error(), "reassign to user ids is required when agent failure action is") {
		t.Errorf("error should quote the server's own text, got: %v", err)
	}
}

func TestValidateApprovalFallback_NotFiresOnReassignToUsersWithUserIds(t *testing.T) {
	steps := approvalStep("agent", map[string]any{
		"agentMode":          "APPROVAL_AGENT_MODE_FULL_CONTROL",
		"agentFailureAction": "APPROVAL_AGENT_FAILURE_ACTION_REASSIGN_TO_USERS",
		"reassignToUserIds":  []any{"u1"},
	})
	if err := validateApprovalFallback(steps, "POLICY_TYPE_GRANT"); err != nil {
		t.Errorf("expected no error: reassignToUserIds is non-empty, got: %v", err)
	}
}

// ---- policyStepsKey ----

func TestPolicyStepsKey(t *testing.T) {
	cases := map[string]string{
		"POLICY_TYPE_GRANT":   "grant",
		"POLICY_TYPE_REVOKE":  "revoke",
		"POLICY_TYPE_CERTIFY": "certify",
	}
	for in, want := range cases {
		got, err := policyStepsKey(in)
		if err != nil {
			t.Errorf("policyStepsKey(%q): unexpected error: %v", in, err)
		}
		if got != want {
			t.Errorf("policyStepsKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPolicyStepsKey_ErrorsOnUnsupportedType(t *testing.T) {
	for _, pt := range []string{"POLICY_TYPE_ACCESS_REQUEST", "POLICY_TYPE_PROVISION", "POLICY_TYPE_UNSPECIFIED", ""} {
		if _, err := policyStepsKey(pt); err == nil {
			t.Errorf("policyStepsKey(%q): expected an error", pt)
		}
	}
}

// ---- mapPolicyType ----

func TestMapPolicyType(t *testing.T) {
	cases := map[string]string{
		"grant":           "POLICY_TYPE_GRANT",
		"GRANT":           "POLICY_TYPE_GRANT",
		"revoke":          "POLICY_TYPE_REVOKE",
		"certify":         "POLICY_TYPE_CERTIFY",
		"access_request":  "POLICY_TYPE_ACCESS_REQUEST",
		"access-request":  "POLICY_TYPE_ACCESS_REQUEST",
		"provision":       "POLICY_TYPE_PROVISION",
		"POLICY_TYPE_FOO": "POLICY_TYPE_FOO", // unrecognized: pass through
	}
	for in, want := range cases {
		if got := mapPolicyType(in); got != want {
			t.Errorf("mapPolicyType(%q) = %q, want %q", in, got, want)
		}
	}
}

// ---- validateCreateBody / validateUpdatePatch integration ----

func TestValidateCreateBody_FiresWithNoPolicySteps(t *testing.T) {
	body := map[string]any{"displayName": "x", "policyType": "POLICY_TYPE_GRANT"}
	if err := validateCreateBody(body, false); err == nil {
		t.Fatal("expected the empty-steps guard to fire for a create body with no policySteps")
	}
}

func TestValidateCreateBody_NotFiresWithRealSteps(t *testing.T) {
	body := map[string]any{
		"displayName": "x",
		"policyType":  "POLICY_TYPE_GRANT",
		"policySteps": map[string]any{
			"grant": map[string]any{"steps": []any{map[string]any{"reject": map[string]any{}}}},
		},
	}
	if err := validateCreateBody(body, false); err != nil {
		t.Errorf("expected no error for a create body with real (even reject) steps, got: %v", err)
	}
}

func TestValidateCreateBody_AllowDenyAllPermitsNoSteps(t *testing.T) {
	body := map[string]any{"displayName": "x", "policyType": "POLICY_TYPE_GRANT"}
	if err := validateCreateBody(body, true); err != nil {
		t.Errorf("expected --allow-deny-all to permit a steps-less create, got: %v", err)
	}
}

func TestValidateUpdatePatch_NotFiresWhenPolicyStepsUntouched(t *testing.T) {
	// A plain display-name update shouldn't trip a guard on a field it
	// never sends.
	patch := map[string]any{"displayName": "new name"}
	if err := validateUpdatePatch(patch, "POLICY_TYPE_GRANT", false); err != nil {
		t.Errorf("expected no error when policySteps isn't part of the patch, got: %v", err)
	}
}

func TestValidateUpdatePatch_FiresWhenPolicyStepsClearedToEmpty(t *testing.T) {
	patch := map[string]any{"policySteps": map[string]any{"grant": map[string]any{"steps": []any{}}}}
	if err := validateUpdatePatch(patch, "POLICY_TYPE_GRANT", false); err == nil {
		t.Fatal("expected the guard to fire when an update clears the baseline steps to empty")
	}
}
