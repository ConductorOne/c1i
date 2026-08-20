package cmd

import (
	"strings"
	"testing"
)

// ---- Guard 1 (C57): empty/missing policySteps ----

func TestValidatePolicyStepsNonEmpty_FiresOnMissing(t *testing.T) {
	err := validatePolicyStepsNonEmpty("POLICY_TYPE_GRANT", nil, false)
	if err == nil {
		t.Fatal("expected an error when policySteps is nil/missing")
	}
	if !strings.Contains(err.Error(), "C57") && !strings.Contains(err.Error(), "deny-everything") {
		t.Errorf("error should explain the C57 deny-all default, got: %v", err)
	}
}

func TestValidatePolicyStepsNonEmpty_FiresOnEmptyStepsArray(t *testing.T) {
	steps := map[string]any{
		"grant": map[string]any{"steps": []any{}},
	}
	err := validatePolicyStepsNonEmpty("POLICY_TYPE_GRANT", steps, false)
	if err == nil {
		t.Fatal("expected an error when the baseline steps array is empty")
	}
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
	if err == nil {
		t.Fatal("expected an error when steps are present but keyed wrong (server would never find them)")
	}
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
	if err == nil {
		t.Fatal("expected --allow-deny-all to NOT bypass an explicit empty steps array (it 500s, not a safe deny-all)")
	}
}

// ---- Guard 2 (C58): policyType required ----

func TestValidatePolicyType_FiresOnEmpty(t *testing.T) {
	if err := validatePolicyType(""); err == nil {
		t.Fatal("expected an error for empty policyType")
	}
}

func TestValidatePolicyType_FiresOnUnspecified(t *testing.T) {
	if err := validatePolicyType("POLICY_TYPE_UNSPECIFIED"); err == nil {
		t.Fatal("expected an error for POLICY_TYPE_UNSPECIFIED")
	}
}

func TestValidatePolicyType_NotFiresOnValidType(t *testing.T) {
	for _, pt := range []string{"POLICY_TYPE_GRANT", "POLICY_TYPE_REVOKE", "POLICY_TYPE_CERTIFY"} {
		if err := validatePolicyType(pt); err != nil {
			t.Errorf("expected no error for %s, got: %v", pt, err)
		}
	}
}

// ---- Guard 3 (C58): empty rules[].condition ----

func TestValidateRuleConditions_FiresOnEmpty(t *testing.T) {
	rules := []any{map[string]any{"condition": "", "stepKey": "grant"}}
	if err := validateRuleConditions(rules); err == nil {
		t.Fatal("expected an error for empty rules[].condition")
	}
}

func TestValidateRuleConditions_FiresOnWhitespaceOnly(t *testing.T) {
	rules := []any{map[string]any{"condition": "   ", "stepKey": "grant"}}
	if err := validateRuleConditions(rules); err == nil {
		t.Fatal("expected an error for a whitespace-only condition")
	}
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
	if err := validateRuleOutcomes(rules); err == nil {
		t.Fatal("expected an error when a rule sets neither stepKey/policyId nor the deprecated policyKey")
	}
}

func TestValidateRuleOutcomes_FiresOnBoth(t *testing.T) {
	rules := []any{map[string]any{"condition": "true", "stepKey": "grant", "policyKey": "grant"}}
	if err := validateRuleOutcomes(rules); err == nil {
		t.Fatal("expected an error when a rule sets both stepKey and the deprecated policyKey")
	}
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
	if err := validateRuleStepKeys(rules, policySteps); err == nil {
		t.Fatal("expected an error: stepKey references a key not present in policySteps")
	}
}

func TestValidateRuleStepKeys_FiresOnKeyWithEmptySteps(t *testing.T) {
	rules := []any{map[string]any{"condition": "true", "stepKey": "escalated"}}
	policySteps := map[string]any{
		"grant":     map[string]any{"steps": []any{map[string]any{"reject": map[string]any{}}}},
		"escalated": map[string]any{"steps": []any{}},
	}
	if err := validateRuleStepKeys(rules, policySteps); err == nil {
		t.Fatal("expected an error: stepKey matches a policySteps entry with no steps")
	}
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
	if err := validateApprovalFallback(steps); err == nil {
		t.Fatal("expected an error: \"users\" does not support fallback")
	}
}

func TestValidateApprovalFallback_FiresOnAppOwnersArmWithFallbackUserIds(t *testing.T) {
	steps := approvalStep("appOwners", map[string]any{"fallbackUserIds": []any{"u1"}})
	if err := validateApprovalFallback(steps); err == nil {
		t.Fatal("expected an error: \"appOwners\" does not support fallbackUserIds")
	}
}

func TestValidateApprovalFallback_FiresOnWebhookArmWithFallback(t *testing.T) {
	steps := approvalStep("webhook", map[string]any{"webhookId": "wh1", "fallback": true})
	if err := validateApprovalFallback(steps); err == nil {
		t.Fatal("expected an error: \"webhook\" does not support fallback")
	}
}

func TestValidateApprovalFallback_NotFiresOnUsersArmWithoutFallback(t *testing.T) {
	steps := approvalStep("users", map[string]any{"userIds": []any{"u1"}})
	if err := validateApprovalFallback(steps); err != nil {
		t.Errorf("expected no error for a plain users arm, got: %v", err)
	}
}

// ---- Guard 5: fallback:true with nothing to fall back to (500 trap) ----

func TestValidateApprovalFallback_FiresOnManagerFallbackTrueEmptyUserIds(t *testing.T) {
	steps := approvalStep("manager", map[string]any{"fallback": true, "fallbackUserIds": []any{}})
	if err := validateApprovalFallback(steps); err == nil {
		t.Fatal("expected an error: manager fallback:true with empty fallbackUserIds 500s server-side")
	}
}

func TestValidateApprovalFallback_FiresOnGroupFallbackTrueGroupEnabledEmptyGroupIds(t *testing.T) {
	steps := approvalStep("group", map[string]any{
		"appGroupId": "g1", "appId": "a1",
		"fallback": true, "isGroupFallbackEnabled": true, "fallbackGroupIds": []any{},
	})
	if err := validateApprovalFallback(steps); err == nil {
		t.Fatal("expected an error: group fallback with isGroupFallbackEnabled but empty fallbackGroupIds 500s")
	}
}

func TestValidateApprovalFallback_NotFiresOnManagerFallbackTrueWithUserIds(t *testing.T) {
	steps := approvalStep("manager", map[string]any{"fallback": true, "fallbackUserIds": []any{"u1"}})
	if err := validateApprovalFallback(steps); err != nil {
		t.Errorf("expected no error: manager fallback:true with real fallbackUserIds, got: %v", err)
	}
}

func TestValidateApprovalFallback_NotFiresOnFallbackFalse(t *testing.T) {
	steps := approvalStep("self", map[string]any{"fallback": false})
	if err := validateApprovalFallback(steps); err != nil {
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
	if err := validateApprovalFallback(steps); err != nil {
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
	if err := validateApprovalFallback(steps); err == nil {
		t.Fatal("expected an error: a provision step is never allowed in a policy body")
	}
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
	if err := validateApprovalFallback(steps); err == nil {
		t.Fatal("expected an error: approval.assigned is read-only and must be false")
	}
}

func TestValidateApprovalFallback_FiresOnManagerAssignedUserIds(t *testing.T) {
	steps := approvalStep("manager", map[string]any{"assignedUserIds": []any{"u1"}})
	if err := validateApprovalFallback(steps); err == nil {
		t.Fatal("expected an error: manager.assignedUserIds is server-computed")
	}
}

func TestValidateApprovalFallback_FiresOnAgentUserID(t *testing.T) {
	steps := approvalStep("agent", map[string]any{"agentUserId": "u1", "agentMode": "APPROVAL_AGENT_MODE_FULL_CONTROL"})
	if err := validateApprovalFallback(steps); err == nil {
		t.Fatal("expected an error: agent.agentUserId is deprecated/system-driven")
	}
}

func TestValidateApprovalFallback_NotFiresOnCleanAgentArm(t *testing.T) {
	steps := approvalStep("agent", map[string]any{"agentMode": "APPROVAL_AGENT_MODE_FULL_CONTROL"})
	if err := validateApprovalFallback(steps); err != nil {
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
	if err := validateApprovalFallback(steps); err != nil {
		t.Errorf("expected no error for a clean approval step, got: %v", err)
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
		t.Fatal("expected the C57 guard to fire for a create body with no policySteps")
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
