package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// TestAppsRemoveOwnerRequiresAppID mirrors TestAppsAddOwnerRequiresAppID: a
// missing/empty --app-id must be rejected as a usage error (exit 2) before
// any network call is attempted.
func TestAppsRemoveOwnerRequiresAppID(t *testing.T) {
	resetCmdFlags(t, appsRemoveOwnerCmd)

	var out bytes.Buffer
	appsRemoveOwnerCmd.SetOut(&out)
	appsRemoveOwnerCmd.SetContext(context.Background())

	err := appsRemoveOwnerCmd.RunE(appsRemoveOwnerCmd, []string{"user1"})
	if err == nil {
		t.Fatal("expected an error when --app-id is unset")
	}
	var ue *usageError
	if !errors.As(err, &ue) {
		t.Errorf("error %v is not a *usageError", err)
	}
	if !strings.Contains(err.Error(), "--app-id") {
		t.Errorf("expected error to mention --app-id, got %v", err)
	}
}

// TestAppsRemoveOwnerDryRunPreviewsRequestWithoutSending proves --dry-run
// prints the exact DELETE and path the real call would send and never
// touches the network.
func TestAppsRemoveOwnerDryRunPreviewsRequestWithoutSending(t *testing.T) {
	resetCmdFlags(t, appsRemoveOwnerCmd)
	t.Setenv("C1I_URL", "https://example.invalid")

	orig := viper.GetBool("dry_run")
	viper.Set("dry_run", true)
	t.Cleanup(func() { viper.Set("dry_run", orig) })

	_ = appsRemoveOwnerCmd.Flags().Set("app-id", "app1")

	var out bytes.Buffer
	appsRemoveOwnerCmd.SetOut(&out)
	appsRemoveOwnerCmd.SetContext(context.Background())

	if err := appsRemoveOwnerCmd.RunE(appsRemoveOwnerCmd, []string{"user1"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "[dry-run] DELETE /api/v1/apps/app1/owners/user1") {
		t.Errorf("missing expected dry-run request line, got %q", got)
	}
	if strings.Contains(got, "{") {
		t.Errorf("remove-owner has no request body; dry-run should not print one, got %q", got)
	}
}
