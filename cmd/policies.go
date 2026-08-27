package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var policiesCmd = &cobra.Command{
	Use:   "policies",
	Short: "Manage policies (approval, provisioning, and certification workflows)",
	Long: `Manage policies — the objects that describe how C1 processes a task
(an access request, a certification, a provisioning action, ...): who approves
it, what happens on escalation or timeout, and how the underlying resource
gets provisioned.

A policy's shape is deeply nested: policySteps holds an ordered list of steps,
each one of seven types (approval, provision, accept, reject, wait, form,
action) — all of which can be read back, while "create"/"update" reject two of
them outright, quoting the refusal verbatim so it greps both ways:
  a "provision" step is never allowed in a policy body — it's read-only and server-computed
  an "action" step is not supported by create/update yet
An approval step's approver is itself a oneof of users/manager/group/
appOwners/self/entitlementOwners/expression/webhook/resourceOwners/agent.
Modeling all of that as flags would be unusable, so "create"/"update" take the
nested pieces from a JSON file (or "-" for stdin) — the same pattern that
"mcp servers register" uses for its auth config — while the flat top-level
fields (display name, description, policy type) stay as flags.

"list" and "search" rows carry step_kinds (the kind of each step in the
policy's baseline sequence) and baseline_policy_id, which together identify a
tenant's auto-approval policy without matching on its display name. See
"policies list --help" for the jq recipe.

Known footgun this command family exists to prevent: POST /api/v1/policies
with only displayName and policyType succeeds and returns a policy whose
steps default to a single reject (deny-everything), with NO validation error.
"create" and "update" refuse to send a request with empty/missing steps
unless you pass --allow-deny-all to say that's what you meant.`,
}

func init() {
	rootCmd.AddCommand(policiesCmd)
}

// newPoliciesClient builds the client every policies subcommand sends
// requests through. It's a var, not a direct newClient call, so a test can
// substitute a client pointed at an httptest.Server (via
// client.NewForTesting) without a real OAuth mint — the same DI pattern
// cmd/requests_create_grant.go's newGrantClient and cmd/api.go's
// newAPIClient use.
var newPoliciesClient = newClient

// mapPolicyType translates a user-friendly --policy-type value to the API
// enum. Input is case-insensitive and accepts '-' or '_' as the word
// separator. Unrecognized input is passed through unchanged so the raw enum
// name (or a deliberately-invalid value, to be caught by validatePolicyType)
// still works.
func mapPolicyType(s string) string {
	switch strings.ToLower(strings.NewReplacer("-", "_").Replace(s)) {
	case "grant":
		return "POLICY_TYPE_GRANT"
	case "revoke":
		return "POLICY_TYPE_REVOKE"
	case "certify":
		return "POLICY_TYPE_CERTIFY"
	case "access_request", "accessrequest":
		return "POLICY_TYPE_ACCESS_REQUEST"
	case "provision":
		return "POLICY_TYPE_PROVISION"
	default:
		return s
	}
}

// readJSONBytes reads raw bytes from path, or stdin when path is "-".
// Shared by every --*-file flag in the policies command family.
func readJSONBytes(cmd *cobra.Command, path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(cmd.InOrStdin())
	}
	return os.ReadFile(path) // #nosec G304 -- user-supplied --*-file path is intentional
}

// readJSONArrayFile reads a JSON array from path (file or "-" for stdin).
// Used for --steps-file (a PolicySteps.steps array) and --rules-file (a
// Rule array) — both are arrays at the top level, unlike --body-file /
// --hosted-config-file elsewhere in this codebase, which read a JSON object
// (see readConfigFile in cmd/mcp_servers_config.go).
func readJSONArrayFile(cmd *cobra.Command, path string) ([]any, error) {
	data, err := readJSONBytes(cmd, path)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}
	var arr []any
	if err := json.Unmarshal(data, &arr); err != nil {
		return nil, fmt.Errorf("parsing as a JSON array: %w", err)
	}
	return arr, nil
}

// policyListItem is the subset of the Policy message (c1.api.policy.v1.Policy)
// that list/search rows are built from. Rules are decoded only far enough to
// count them; steps far enough to count them and name each one's kind
// (step_count/rule_count/step_kinds), not to re-emit the full nested
// structure in NDJSON rows.
type policyListItem struct {
	ID            string `json:"id"`
	DisplayName   string `json:"displayName"`
	Description   string `json:"description"`
	PolicyType    string `json:"policyType"`
	SystemBuiltin bool   `json:"systemBuiltin"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
	DeletedAt     string `json:"deletedAt"`
	// Set only on a POLICY_REFERENCES_POLICY tenant, and mutually exclusive
	// with the baseline policySteps entry (policy.proto): such a policy has no
	// baseline sequence of its own, so step_kinds is legitimately empty.
	BaselinePolicyID string `json:"baselinePolicyId"`
	Rules            []struct {
		Condition string `json:"condition"`
	} `json:"rules"`
	PolicySteps map[string]struct {
		Steps []json.RawMessage `json:"steps"`
	} `json:"policySteps"`
}

// stepKind returns a policy step's protojson oneof name — one of stepArms
// (cmd/policies_validate.go), which mirrors the seven members policy.proto
// declares. All seven are readable here; that create/update refuses to *write*
// an "action" step is a separate, write-side restriction. An unrecognized name
// passes through as itself, so a member added later needs no code change. A
// step object carries exactly one key (the oneof), so any other shape is
// reported as "unknown", which fails closed: a caller matching on a specific
// kind never matches a step we could not read.
func stepKind(raw json.RawMessage) string {
	var step map[string]json.RawMessage
	if err := json.Unmarshal(raw, &step); err != nil || len(step) != 1 {
		return "unknown"
	}
	for k := range step {
		return k
	}
	return "unknown"
}

// policyBaselineKey is the policySteps key holding the sequence that runs when
// no conditional rule matches: the lowercased policy type ("grant", "revoke",
// "certify"), per policy.proto's policy_steps/policy_type docs.
func policyBaselineKey(policyType string) string {
	return strings.ToLower(strings.TrimPrefix(policyType, "POLICY_TYPE_"))
}

// policyStepKinds names the kinds of the BASELINE step sequence, in order.
//
// policySteps is not a map of phases: exactly one entry is the baseline, keyed
// by the lowercased policy type, and any further entries have opaque UUID keys
// naming alternative sequences that `rules` routes to conditionally. Only one
// sequence ever executes, so flattening them together would describe a run
// that never happens — and would sort UUID keys ahead of "grant" (digits
// precede letters), putting a conditional branch's step first.
//
// The baseline is therefore the only sequence reportable as "the policy's
// steps". When conditional rules exist the baseline is still what runs by
// default, and rule_count is the caller's signal that other sequences exist.
// This is why step_kinds and step_count legitimately differ.
//
// The result is always non-nil, so step_kinds renders as [] rather than null.
// A policy with no baseline entry yields an empty list rather than a guess —
// again failing closed. At least three things produce that, and only the row's
// other keys tell them apart: an unset or unrecognized policy type; a policy
// that genuinely has no baseline; and, on a POLICY_REFERENCES_POLICY tenant, a
// policy that delegates its baseline to another via baselinePolicyId, which
// policy.proto makes mutually exclusive with a baseline entry. The last is a
// working policy, not a broken one, and rule_count does not distinguish it
// (it is 0 unless the policy also has conditional rules) — baseline_policy_id
// on the row is what does.
func policyStepKinds(p policyListItem) []string {
	baseline := p.PolicySteps[policyBaselineKey(p.PolicyType)]

	kinds := make([]string, 0, len(baseline.Steps))
	for _, raw := range baseline.Steps {
		kinds = append(kinds, stepKind(raw))
	}
	return kinds
}

// policyStepCount counts every step in every policySteps entry — the baseline
// and each conditionally-routed alternative. It predates step_kinds and is
// deliberately left alone: narrowing it to the baseline would silently change
// the value emitted for any policy with conditional routing.
func policyStepCount(p policyListItem) int {
	stepCount := 0
	for _, steps := range p.PolicySteps {
		stepCount += len(steps.Steps)
	}
	return stepCount
}

// policyRow flattens a policyListItem into the NDJSON output row shared by
// `policies list` and `policies search`. system_builtin/rule_count/step_count
// keep their real JSON types (bool/int) rather than being stringified — see
// CLAUDE.md's "row values keep their real JSON types" convention.
//
// step_kinds exists because policy_type/step_count/system_builtin cannot tell
// an auto-approval policy from an approval gate: on a live tenant both are
// POLICY_TYPE_GRANT, system_builtin, one step. Only the step's kind separates
// them (accept vs approval), and the list response already carries it.
// It covers the baseline sequence only — see policyStepKinds — so it does not
// necessarily hold step_count entries.
func policyRow(p policyListItem) map[string]any {
	stepKinds := policyStepKinds(p)
	// deleted_at is nil, not "", on a live policy: `jq 'select(.deleted_at)'`
	// must not match one. An empty string is truthy in jq, so emitting "" here
	// would silently select every row.
	var deletedAt any
	if p.DeletedAt != "" {
		deletedAt = p.DeletedAt
	}
	// Same nil-not-"" reason as deleted_at, and the same jq consequence: this
	// is the key that tells an empty step_kinds caused by delegation apart
	// from one caused by a policy with no baseline at all.
	var baselinePolicyID any
	if p.BaselinePolicyID != "" {
		baselinePolicyID = p.BaselinePolicyID
	}
	return map[string]any{
		"id":                 p.ID,
		"display_name":       p.DisplayName,
		"description":        p.Description,
		"policy_type":        p.PolicyType,
		"system_builtin":     p.SystemBuiltin,
		"rule_count":         len(p.Rules),
		"step_count":         policyStepCount(p),
		"step_kinds":         stepKinds,
		"created_at":         p.CreatedAt,
		"updated_at":         p.UpdatedAt,
		"deleted_at":         deletedAt,
		"baseline_policy_id": baselinePolicyID,
	}
}
