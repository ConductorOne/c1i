package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spf13/cobra"
)

func newPoliciesCreateFlagCmd(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.Flags().String("display-name", "", "")
	cmd.Flags().String("description", "", "")
	cmd.Flags().String("policy-type", "", "")
	cmd.Flags().String("steps-file", "", "")
	cmd.Flags().String("rules-file", "", "")
	cmd.Flags().String("body-file", "", "")
	cmd.Flags().Bool("allow-deny-all", false, "")
	return cmd
}

func writeTempJSON(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	return path
}

// TestBuildCreatePolicyBodyFromFlags pins the flat body shape and confirms
// --steps-file is wrapped under the LOWERCASE baseline key ("grant"), not
// the enum constant — the exact mistake that would silently recreate C57
// even though steps were supplied.
func TestBuildCreatePolicyBodyFromFlags(t *testing.T) {
	cmd := newPoliciesCreateFlagCmd(t)
	_ = cmd.Flags().Set("display-name", "My Policy")
	_ = cmd.Flags().Set("policy-type", "grant")
	stepsPath := writeTempJSON(t, "steps.json", `[{"approval":{"users":{"userIds":["u1"]}}}]`)
	_ = cmd.Flags().Set("steps-file", stepsPath)

	body, err := buildCreatePolicyBody(cmd)
	if err != nil {
		t.Fatalf("buildCreatePolicyBody: %v", err)
	}
	if body["displayName"] != "My Policy" || body["policyType"] != "POLICY_TYPE_GRANT" {
		t.Errorf("unexpected top-level fields: %v", body)
	}
	ps, ok := body["policySteps"].(map[string]any)
	if !ok {
		t.Fatalf("policySteps missing or wrong type: %v", body["policySteps"])
	}
	if _, ok := ps["POLICY_TYPE_GRANT"]; ok {
		t.Errorf("policySteps must NOT be keyed by the enum constant: %v", ps)
	}
	grantEntry, ok := ps["grant"].(map[string]any)
	if !ok {
		t.Fatalf(`policySteps must be keyed by "grant" (lowercase), got: %v`, ps)
	}
	steps, ok := grantEntry["steps"].([]any)
	if !ok || len(steps) != 1 {
		t.Errorf("expected exactly 1 step under policySteps.grant.steps, got: %v", grantEntry)
	}
}

// TestBuildCreatePolicyBodyOmitsEmptyDescription mirrors
// TestBuildAppCreateBodyMinimal's convention: optional fields aren't sent as
// empty strings.
func TestBuildCreatePolicyBodyOmitsEmptyDescription(t *testing.T) {
	cmd := newPoliciesCreateFlagCmd(t)
	_ = cmd.Flags().Set("display-name", "x")
	_ = cmd.Flags().Set("policy-type", "grant")

	body, err := buildCreatePolicyBody(cmd)
	if err != nil {
		t.Fatalf("buildCreatePolicyBody: %v", err)
	}
	if _, ok := body["description"]; ok {
		t.Errorf("description should be omitted when not set, got: %v", body)
	}
	if _, ok := body["policySteps"]; ok {
		t.Errorf("policySteps should be omitted when --steps-file isn't set, got: %v", body)
	}
}

func TestBuildCreatePolicyBodyRequiresDisplayName(t *testing.T) {
	cmd := newPoliciesCreateFlagCmd(t)
	_ = cmd.Flags().Set("policy-type", "grant")
	if _, err := buildCreatePolicyBody(cmd); err == nil {
		t.Fatal("expected an error when --display-name is omitted (and no --body-file)")
	}
}

func TestBuildCreatePolicyBodyFromBodyFile(t *testing.T) {
	cmd := newPoliciesCreateFlagCmd(t)
	bodyPath := writeTempJSON(t, "body.json", `{"displayName":"x","policyType":"POLICY_TYPE_GRANT","policySteps":{"grant":{"steps":[{"reject":{}}]}}}`)
	_ = cmd.Flags().Set("body-file", bodyPath)

	body, err := buildCreatePolicyBody(cmd)
	if err != nil {
		t.Fatalf("buildCreatePolicyBody: %v", err)
	}
	want := map[string]any{
		"displayName": "x",
		"policyType":  "POLICY_TYPE_GRANT",
		"policySteps": map[string]any{
			"grant": map[string]any{"steps": []any{map[string]any{"reject": map[string]any{}}}},
		},
	}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %#v, want %#v", body, want)
	}
}

func TestBuildCreatePolicyBodyFileMutuallyExclusiveWithFlags(t *testing.T) {
	cmd := newPoliciesCreateFlagCmd(t)
	bodyPath := writeTempJSON(t, "body.json", `{}`)
	_ = cmd.Flags().Set("body-file", bodyPath)
	_ = cmd.Flags().Set("display-name", "x")

	if _, err := buildCreatePolicyBody(cmd); err == nil {
		t.Fatal("expected an error: --body-file and --display-name are mutually exclusive")
	}
}

// TestPoliciesCreateStepsFileWrongPolicyTypeRefused proves --steps-file is
// refused up front for a policy type that can't own a top-level baseline
// entry (server-internal / deprecated types), rather than silently sending
// a request the server would reject with a bare error.
func TestPoliciesCreateStepsFileWrongPolicyTypeRefused(t *testing.T) {
	cmd := newPoliciesCreateFlagCmd(t)
	_ = cmd.Flags().Set("display-name", "x")
	_ = cmd.Flags().Set("policy-type", "provision")
	stepsPath := writeTempJSON(t, "steps.json", `[{"reject":{}}]`)
	_ = cmd.Flags().Set("steps-file", stepsPath)

	if _, err := buildCreatePolicyBody(cmd); err == nil {
		t.Fatal("expected an error: provision does not support a top-level policySteps entry via --steps-file")
	}
}
