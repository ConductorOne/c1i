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

A policy's shape is deeply nested (policySteps holds an ordered list of steps,
each a oneof of approval/provision/accept/reject/wait/form; an approval step's
approver is itself a oneof of users/manager/group/appOwners/self/
entitlementOwners/expression/webhook/resourceOwners/agent). Modeling all of
that as flags would be unusable, so "create"/"update" take the nested pieces
from a JSON file (or "-" for stdin) — the same pattern "mcp servers register"
uses for its auth config — while the flat top-level fields (display name,
description, policy type) stay as flags.

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
	return os.ReadFile(path) //nolint:gosec // user-supplied file path is intentional
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
// that list/search rows are built from. PolicySteps and Rules are decoded
// only far enough to count them (step_count/rule_count), not to re-emit the
// full nested structure in NDJSON rows.
type policyListItem struct {
	ID            string `json:"id"`
	DisplayName   string `json:"displayName"`
	Description   string `json:"description"`
	PolicyType    string `json:"policyType"`
	SystemBuiltin bool   `json:"systemBuiltin"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
	DeletedAt     string `json:"deletedAt"`
	Rules         []struct {
		Condition string `json:"condition"`
	} `json:"rules"`
	PolicySteps map[string]struct {
		Steps []json.RawMessage `json:"steps"`
	} `json:"policySteps"`
}

// policyRow flattens a policyListItem into the NDJSON output row shared by
// `policies list` and `policies search`. system_builtin/rule_count/step_count
// keep their real JSON types (bool/int) rather than being stringified — see
// CLAUDE.md's "row values keep their real JSON types" convention.
func policyRow(p policyListItem) map[string]any {
	stepCount := 0
	for _, steps := range p.PolicySteps {
		stepCount += len(steps.Steps)
	}
	return map[string]any{
		"id":             p.ID,
		"display_name":   p.DisplayName,
		"description":    p.Description,
		"policy_type":    p.PolicyType,
		"system_builtin": p.SystemBuiltin,
		"rule_count":     len(p.Rules),
		"step_count":     stepCount,
		"created_at":     p.CreatedAt,
		"updated_at":     p.UpdatedAt,
	}
}
