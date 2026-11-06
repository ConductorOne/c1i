package cmd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newAppCreateFlagCmd() *cobra.Command {
	cmd := &cobra.Command{}
	f := cmd.Flags()
	for _, name := range []string{"display-name", "description", "grant-policy-id", "revoke-policy-id", "certify-policy-id", "identity-matching"} {
		f.String(name, "", "")
	}
	return cmd
}

// TestBuildAppCreateBodyMinimal pins that only displayName is sent when nothing
// else is supplied (no empty description key leaking into the request).
func TestBuildAppCreateBodyMinimal(t *testing.T) {
	cmd := newAppCreateFlagCmd()
	_ = cmd.Flags().Set("display-name", "Google Workspace")

	got, err := buildAppCreateBody(cmd)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"displayName": "Google Workspace"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
}

// TestBuildAppCreateBodyFull pins optional description flows through when set.
func TestBuildAppCreateBodyFull(t *testing.T) {
	cmd := newAppCreateFlagCmd()
	_ = cmd.Flags().Set("display-name", "Acme")
	_ = cmd.Flags().Set("description", "the app")

	got, err := buildAppCreateBody(cmd)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"displayName": "Acme", "description": "the app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
}

// TestBuildAppCreateBodyPoliciesAndIdentityMatching pins the policy ids and the
// mapped identity-matching enum on CreateAppRequest's top-level keys.
func TestBuildAppCreateBodyPoliciesAndIdentityMatching(t *testing.T) {
	cmd := newAppCreateFlagCmd()
	_ = cmd.Flags().Set("display-name", "Acme")
	_ = cmd.Flags().Set("grant-policy-id", "gp-1")
	_ = cmd.Flags().Set("revoke-policy-id", "rp-1")
	_ = cmd.Flags().Set("certify-policy-id", "cp-1")
	_ = cmd.Flags().Set("identity-matching", "Display-Name")

	got, err := buildAppCreateBody(cmd)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"displayName":      "Acme",
		"grantPolicyId":    "gp-1",
		"revokePolicyId":   "rp-1",
		"certifyPolicyId":  "cp-1",
		"identityMatching": "APP_USER_IDENTITY_MATCHING_DISPLAY_NAME",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
}

func TestBuildAppCreateBodyRejectsUnknownIdentityMatching(t *testing.T) {
	cmd := newAppCreateFlagCmd()
	_ = cmd.Flags().Set("display-name", "Acme")
	_ = cmd.Flags().Set("identity-matching", "fuzzy")

	if _, err := buildAppCreateBody(cmd); err == nil || !strings.Contains(err.Error(), "invalid --identity-matching") {
		t.Errorf("err = %v, want invalid --identity-matching", err)
	}
}
