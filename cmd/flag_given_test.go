package cmd

import "testing"

// An exact "" on an emptyIsOmitted flag is "not given", so it must not trip
// the --body-file exclusivity check (it did when bare presence was the signal).
func TestBuildCreatePolicyBodyFileIgnoresEmptyOmittedFlags(t *testing.T) {
	cmd := newPoliciesCreateFlagCmd(t)
	allowEmpty(cmd, emptyIsOmitted, "description", "steps-file", "rules-file")
	_ = cmd.Flags().Set("body-file", writeTempJSON(t, "body.json", `{"displayName":"x"}`))
	for _, n := range []string{"description", "steps-file", "rules-file"} {
		_ = cmd.Flags().Set(n, "")
	}
	if _, err := buildCreatePolicyBody(cmd); err != nil {
		t.Fatalf("--body-file with empty omitted flags: %v", err)
	}
	_ = cmd.Flags().Set("description", "d")
	if _, err := buildCreatePolicyBody(cmd); err == nil {
		t.Fatal("--body-file with --description d must stay exclusive")
	}
}

func TestBuildExternalConfigFileIgnoresEmptyOmittedFlags(t *testing.T) {
	path := writeTempJSON(t, "ext.json", `{"url":"https://example.invalid/mcp"}`)
	cmd := newServerFlagCmd()
	allowEmpty(cmd, emptyIsOmitted, "server-url", "transport")
	_ = cmd.Flags().Set("external-config-file", path)
	for _, n := range []string{"bearer-token", "server-url", "transport"} {
		_ = cmd.Flags().Set(n, "")
	}
	if _, err := buildExternalConfig(cmd); err != nil {
		t.Fatalf("--external-config-file with empty omitted flags: %v", err)
	}
	_ = cmd.Flags().Set("bearer-token", "SECRET")
	if _, err := buildExternalConfig(cmd); err == nil {
		t.Fatal("--external-config-file with a real --bearer-token must stay exclusive")
	}
}
