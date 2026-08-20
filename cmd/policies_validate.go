package cmd

import (
	"fmt"
	"strings"
)

// This file holds the client-side guards for `policies create`/`update`
// : checks that run BEFORE any request is sent, each
// failing as a *usageError (exit 2). They exist because several C1 policy
// API defects either silently produce a dangerous result (an empty
// policySteps becomes a deny-everything policy, no error) or return an
// unhelpful status. Most of the rejections this file pins are plain
// fmt.Errorf values from pkg/models/policy/policy_validate.go's
// validateApproval/ValidateNonProvisionPolicyStepSlice — NOT gRPC-status
// errors — and the platform's generic error-to-HTTP-status mapping falls
// back to codes.Unknown for a bare error, which maps to HTTP 500. So every
// guard below is not just "the server 400s, catch it earlier" but "the
// server 500s with no explanation, catch it client-side or the caller gets
// an opaque failure." Verified against the platform source
// (pkg/models/policy/policy.go, policy_validate.go,
// pkg/controller/conditional_policy/controller/conditional_policy.go) and
// the live public OpenAPI spec, not guessed.

// approvalArms lists every arm of the Approval oneof (c1.api.policy.v1.
// Approval.typ), in API declaration order.
var approvalArms = []string{
	"users", "manager", "appOwners", "group", "self",
	"entitlementOwners", "expression", "webhook", "resourceOwners", "agent",
}

// approverArmsWithoutFallback lists the Approval arms whose message type
// declares NO fallback/fallbackUserIds fields at all: UserApproval,
// AppOwnerApproval, WebhookApproval, AgentApproval. Sending fallback /
// fallbackUserIds on one of these is rejected server-side as an unknown
// JSON field at protojson-unmarshal time (400), before the handler runs.
//
// The other six arms (manager, group, self, entitlementOwners, expression,
// resourceOwners) each declare their OWN fallback + fallbackUserIds +
// fallbackGroupIds + isGroupFallbackEnabled fields, validated identically —
// contrary to an earlier assumption that fallback was manager-only. Verified
// against platform source (pkg/models/policy/policy_validate.go's
// validateApproval, one fallback-arm case per type) and independently
// against the live public OpenAPI schema.
var approverArmsWithoutFallback = map[string]bool{
	"users":     true,
	"appOwners": true,
	"webhook":   true,
	"agent":     true,
}

// approverArmsWithFallback lists the six arms that DO carry fallback /
// fallbackUserIds / fallbackGroupIds / isGroupFallbackEnabled, each
// validated the same way server-side (see validateApprovalFallback).
var approverArmsWithFallback = map[string]bool{
	"manager":           true,
	"group":             true,
	"self":              true,
	"entitlementOwners": true,
	"expression":        true,
	"resourceOwners":    true,
}

// policyStepsKey returns the policySteps map key for a policy's BASELINE
// steps entry, given its (already API-enum-mapped) --policy-type value.
//
// This is not the enum constant. Per pkg/models/policy/policy.go's
// PolicyStepsTypeKey (platform source, verified), the baseline key is the
// lowercase type word — "grant", "revoke", or "certify" — and the function
// errors for any other policy type. Getting this wrong is worse than it
// looks: a caller who supplies --steps-file with real steps, but has them
// keyed under the wrong map key (e.g. the literal enum string
// "POLICY_TYPE_GRANT"), ends up with a request the server can't match to
// the baseline (HasSteps checks policy.PolicySteps[key] by this exact key) —
// which falls back to EXACTLY the deny-all default this command family
// exists to prevent, even though the caller did supply steps. So this
// mapping is applied unconditionally when building policySteps from
// --steps-file; a caller supplying the full body verbatim via --body-file
// is responsible for keying it correctly themselves.
//
// POLICY_TYPE_PROVISION is server-internal (not configured as a top-level
// policy) and POLICY_TYPE_ACCESS_REQUEST is deprecated; neither supports a
// caller-supplied top-level policySteps entry via --steps-file.
func policyStepsKey(policyType string) (string, error) {
	switch policyType {
	case "POLICY_TYPE_GRANT":
		return "grant", nil
	case "POLICY_TYPE_REVOKE":
		return "revoke", nil
	case "POLICY_TYPE_CERTIFY":
		return "certify", nil
	default:
		return "", fmt.Errorf("--policy-type %q does not support a top-level policySteps entry via --steps-file (only grant, revoke, and certify do; provision is server-internal and access_request is deprecated) — pass the full policySteps map yourself via --body-file if you need it", policyType)
	}
}

// validatePolicyType guards policyType: is effectively required (the
// server 400s without it, via a validate.rules enum{not_in:[0]} constraint
// enforced before the handler runs) even though it happens to be absent
// from the OpenAPI schema's declared "required" list.
func validatePolicyType(policyType string) error {
	if policyType == "" || policyType == "POLICY_TYPE_UNSPECIFIED" {
		return &usageError{fmt.Errorf("--policy-type is required and must not be unspecified: the server 400s without it")}
	}
	return nil
}

// validatePolicyStepsNonEmpty guards a specific policyType's
// baseline entry, plus a related crash the original write-up didn't cover.
//
// Two distinct server behaviors are in play, verified against
// pkg/models/policy/policy.go:
//
//  1. HasSteps only checks whether the baseline KEY (policyStepsKey(policyType))
//     is PRESENT in the map — not whether its steps are non-empty. When the
//     key is absent (and baselinePolicyId is unset), EnsureBaselineSteps
//     silently injects a single {"reject":{}} step (deny-everything, no
//     error) — this is the silent deny-all default.
//  2. When the key IS present but its steps array is explicitly empty,
//     EnsureBaselineSteps does NOT fire (HasSteps already sees the key), and
//     the server rejects it cleanly: 400, "invalid PolicySteps.Steps: value
//     must contain at least 1 item(s)" (verified live). So an empty array is
//     the SAFER mistake — it fails loudly, where an absent key succeeds
//     silently. We still pre-flight it to save a round trip and name the
//     remedy, and allowDenyAll does not bypass it because an empty array is
//     never what a caller actually wants.
//
// allowDenyAll (--allow-deny-all) only bypasses case 1 — the caller's
// explicit opt-in to accept the server's silent deny-all default.
func validatePolicyStepsNonEmpty(policyType string, policySteps map[string]any, allowDenyAll bool) error {
	for key, v := range policySteps {
		entry, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if raw, present := entry["steps"]; present {
			arr, _ := raw.([]any)
			if len(arr) == 0 {
				return &usageError{fmt.Errorf("policySteps[%q].steps is explicitly empty: the server rejects this with a 400 (%q must contain at least 1 item). Supply a real step, or omit the %q key entirely and pass --allow-deny-all if you intend a deny-all policy", key, "steps", key)}
			}
		}
	}
	if allowDenyAll {
		return nil
	}
	baselineKey, err := policyStepsKey(policyType)
	if err != nil {
		// An unmappable policyType (access_request/provision/unspecified) is
		// caught by validatePolicyType or policyStepsKey's own caller
		// (buildCreatePolicyBody) when --steps-file is used; nothing further
		// to check here.
		return nil
	}
	entry, ok := policySteps[baselineKey].(map[string]any)
	hasBaseline := ok
	if hasBaseline {
		if arr, _ := entry["steps"].([]any); len(arr) == 0 {
			hasBaseline = false
		}
	}
	if !hasBaseline {
		return &usageError{fmt.Errorf("policySteps[%q] is missing (or has no steps): the server silently creates a deny-everything policy (a single {\"reject\":{}} step) under this key when it's absent, with no validation error. Pass --steps-file with at least one real step for the %q key, or --allow-deny-all if a deny-all policy is what you actually want", baselineKey, baselineKey)}
	}
	return nil
}

// validateRuleOutcomes guards a routing-graph constraint: each rule in
// rules[] must set EXACTLY ONE outcome — the modern "stepKey" (or its
// "policyId" sibling, not modeled by --rules-file today) or the deprecated
// flat "policyKey" — never both, never neither. Confirmed against
// pkg/controller/conditional_policy/controller/conditional_policy.go, which
// returns a structured PolicyGraphErrorAmbiguous / PolicyGraphErrorEmpty for
// this (already a clean 400, unlike the bare-error guards above — this
// check just saves the round trip and gives a clearer message).
func validateRuleOutcomes(rules []any) error {
	for i, r := range rules {
		rule, ok := r.(map[string]any)
		if !ok {
			continue
		}
		_, hasStepKey := rule["stepKey"]
		_, hasPolicyID := rule["policyId"]
		_, hasLegacyKey := rule["policyKey"]
		hasOutcome := hasStepKey || hasPolicyID
		switch {
		case hasOutcome && hasLegacyKey:
			return &usageError{fmt.Errorf("rules[%d]: cannot set both the deprecated policyKey field and stepKey/policyId; clear policyKey when writing the new shape", i)}
		case !hasOutcome && !hasLegacyKey:
			return &usageError{fmt.Errorf("rules[%d]: must set exactly one outcome (stepKey or policyId; policyKey is the deprecated alias for stepKey)", i)}
		}
	}
	return nil
}

// validateRuleStepKeys guards another bare-error (-> HTTP 500) trap: a
// rule's "stepKey" must reference a policySteps entry that exists AND has
// non-empty steps. Confirmed against
// pkg/controller/conditional_policy/controller/conditional_policy.go's
// Controller.Validate, which returns the package-level
// `ErrNoStepsForRule = errors.New("no steps for rule")` for this — a bare
// error, unlike the sibling CEL-compile-failure branch in the same
// function, which properly returns status.Error(codes.InvalidArgument, ...).
// Only checked when policySteps is available (i.e. being built/replaced in
// the same request) — a rule referencing a step key that already exists on
// the policy server-side, unrelated to this update, isn't something this
// client-side check can see.
func validateRuleStepKeys(rules []any, policySteps map[string]any) error {
	if policySteps == nil {
		return nil
	}
	for i, r := range rules {
		rule, ok := r.(map[string]any)
		if !ok {
			continue
		}
		stepKey, _ := rule["stepKey"].(string)
		if stepKey == "" {
			continue // policyId outcome, or the deprecated policyKey — not checked here
		}
		entry, ok := policySteps[stepKey].(map[string]any)
		if !ok {
			return &usageError{fmt.Errorf("rules[%d].stepKey %q does not match any key in policySteps — the server returns a bare \"no steps for rule\" error (HTTP 500) for this, not a 400", i, stepKey)}
		}
		if steps, _ := entry["steps"].([]any); len(steps) == 0 {
			return &usageError{fmt.Errorf("rules[%d].stepKey %q matches a policySteps entry with no steps — the server returns a bare \"no steps for rule\" error (HTTP 500) for this, not a 400", i, stepKey)}
		}
	}
	return nil
}

// validateRuleConditions guards an empty rules[].condition 400s with a
// length-bounds message that doesn't explain the fix — a baseline/catch-all
// rule needs the literal condition "true", not an empty string.
func validateRuleConditions(rules []any) error {
	for i, r := range rules {
		rule, ok := r.(map[string]any)
		if !ok {
			continue
		}
		cond, _ := rule["condition"].(string)
		if strings.TrimSpace(cond) == "" {
			return &usageError{fmt.Errorf(`rules[%d].condition is empty: the server 400s with a length-bounds error that doesn't explain this — a baseline/catch-all rule needs the literal condition "true", not an empty string`, i)}
		}
	}
	return nil
}

// validateApprovalFallback walks every step in policySteps and guards the
// bare-error (-> HTTP 500) traps in pkg/models/policy/policy_validate.go's
// validateApproval, plus the provision-step rejection in
// ValidateNonProvisionPolicyStepSlice:
//
//  1. A `provision` step is never allowed in a policy body (it's a
//     read-only, server-computed step type) — "provision steps are not
//     allowed in policies", a bare error.
//  2. `approval.assigned` must not be sent as true — it's a read-only,
//     server-computed field ("assigned is a read-only field and must be
//     false by default").
//  3. fallback / fallbackUserIds set on an arm that doesn't support them
//     (users, appOwners, webhook, agent) — rejected server-side as an
//     unknown JSON field (400, not 500, but still worth catching early).
//  4. fallback:true with nothing to fall back to, on any of the six arms
//     that support it (manager, group, self, expression, entitlementOwners,
//     resourceOwners) — each is validated identically: fallbackGroupIds
//     must be non-empty when isGroupFallbackEnabled is true, otherwise
//     fallbackUserIds must be non-empty. A bare error either way.
//  5. `manager.assignedUserIds` must be empty — server-computed
//     ("manager steps assigned user IDs is set by the task workflow, not
//     defined on the input").
//  6. `agent.agentUserId` must be empty — deprecated/system-driven
//     ("agent approval steps are system-driven and cannot reference an
//     agent user").
//
// A handful of additional agent-approval business rules exist server-side
// (agentMode/agentFailureAction required, mode<->policyType constraints,
// reassign/policy-id list requirements when specific modes are chosen) that
// this guard does not yet cover — see the policies_validate.go package
// comment / README for the callout.
func validateApprovalFallback(policySteps map[string]any) error {
	for ptype, v := range policySteps {
		entry, ok := v.(map[string]any)
		if !ok {
			continue
		}
		steps, _ := entry["steps"].([]any)
		for i, s := range steps {
			step, ok := s.(map[string]any)
			if !ok {
				continue
			}
			if _, hasProvision := step["provision"]; hasProvision {
				return &usageError{fmt.Errorf("policySteps[%q].steps[%d]: a \"provision\" step is never allowed in a policy body — it's read-only and server-computed", ptype, i)}
			}
			approval, ok := step["approval"].(map[string]any)
			if !ok {
				continue
			}
			if assigned, _ := approval["assigned"].(bool); assigned {
				return &usageError{fmt.Errorf("policySteps[%q].steps[%d].approval.assigned must not be true — it's a read-only, server-computed field", ptype, i)}
			}
			for _, arm := range approvalArms {
				armObj, ok := approval[arm].(map[string]any)
				if !ok {
					continue
				}
				_, hasFallback := armObj["fallback"]
				_, hasFallbackUserIDs := armObj["fallbackUserIds"]
				if approverArmsWithoutFallback[arm] && (hasFallback || hasFallbackUserIDs) {
					return &usageError{fmt.Errorf("policySteps[%q].steps[%d].approval.%s does not support fallback/fallbackUserIds (only manager, group, self, expression, entitlementOwners, and resourceOwners do) — the server rejects it as an unknown field", ptype, i, arm)}
				}
				if approverArmsWithFallback[arm] {
					fallback, _ := armObj["fallback"].(bool)
					if fallback {
						groupFallbackEnabled, _ := armObj["isGroupFallbackEnabled"].(bool)
						if groupFallbackEnabled {
							groupIDs, _ := armObj["fallbackGroupIds"].([]any)
							if len(groupIDs) == 0 {
								return &usageError{fmt.Errorf("policySteps[%q].steps[%d].approval.%s has fallback:true and isGroupFallbackEnabled:true but fallbackGroupIds is empty — the server returns HTTP 500 for this instead of a 400", ptype, i, arm)}
							}
						} else {
							userIDs, _ := armObj["fallbackUserIds"].([]any)
							if len(userIDs) == 0 {
								return &usageError{fmt.Errorf("policySteps[%q].steps[%d].approval.%s has fallback:true but fallbackUserIds is empty — the server returns HTTP 500 for this instead of a 400", ptype, i, arm)}
							}
						}
					}
				}
				if arm == "manager" {
					if ids, _ := armObj["assignedUserIds"].([]any); len(ids) > 0 {
						return &usageError{fmt.Errorf("policySteps[%q].steps[%d].approval.manager.assignedUserIds must be empty — it's set by the task workflow at runtime, not on the policy definition", ptype, i)}
					}
				}
				if arm == "agent" {
					if uid, _ := armObj["agentUserId"].(string); uid != "" {
						return &usageError{fmt.Errorf("policySteps[%q].steps[%d].approval.agent.agentUserId must be empty — agent approval steps are system-driven and cannot reference an agent user (deprecated field)", ptype, i)}
					}
				}
			}
		}
	}
	return nil
}

// validateCreateBody runs every guard against a fully-built CreatePolicyRequest
// body (from either flags or --body-file). policyType and policySteps are
// always checked (a create always needs both, absent --allow-deny-all for
// the latter); rules are only checked when present, since they're optional.
func validateCreateBody(body map[string]any, allowDenyAll bool) error {
	policyType, _ := body["policyType"].(string)
	if err := validatePolicyType(policyType); err != nil {
		return err
	}
	policySteps, _ := body["policySteps"].(map[string]any)
	if err := validatePolicyStepsNonEmpty(policyType, policySteps, allowDenyAll); err != nil {
		return err
	}
	if err := validateApprovalFallback(policySteps); err != nil {
		return err
	}
	if rules, ok := body["rules"].([]any); ok {
		if err := validateRuleOutcomes(rules); err != nil {
			return err
		}
		if err := validateRuleConditions(rules); err != nil {
			return err
		}
		if err := validateRuleStepKeys(rules, policySteps); err != nil {
			return err
		}
	}
	return nil
}

// validateUpdatePatch runs the same guards as validateCreateBody, but only
// against the fields actually present in a partial update patch — an
// update that isn't touching policyType/policySteps/rules at all shouldn't
// be blocked by a guard on a field it never sends. policyType falls back to
// fallbackPolicyType (the existing policy's type, when known) so the
// policySteps guard can still resolve the right baseline key when
// --steps-file is supplied without --policy-type on an update.
func validateUpdatePatch(policy map[string]any, fallbackPolicyType string, allowDenyAll bool) error {
	policyType, hasPolicyType := policy["policyType"].(string)
	if hasPolicyType {
		if err := validatePolicyType(policyType); err != nil {
			return err
		}
	} else {
		policyType = fallbackPolicyType
	}
	ps, hasSteps := policy["policySteps"].(map[string]any)
	if hasSteps {
		if err := validatePolicyStepsNonEmpty(policyType, ps, allowDenyAll); err != nil {
			return err
		}
		if err := validateApprovalFallback(ps); err != nil {
			return err
		}
	}
	if rules, ok := policy["rules"].([]any); ok {
		if err := validateRuleOutcomes(rules); err != nil {
			return err
		}
		if err := validateRuleConditions(rules); err != nil {
			return err
		}
		if hasSteps {
			if err := validateRuleStepKeys(rules, ps); err != nil {
				return err
			}
		}
	}
	return nil
}
