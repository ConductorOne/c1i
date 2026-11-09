package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// TestAppsRemoveOwnerRequiresUserID mirrors TestAppsAddOwnerRequiresUserID: a
// missing --user-id must be rejected as a usage error (exit 2) before
// any network call is attempted.
func TestAppsRemoveOwnerRequiresUserID(t *testing.T) {
	// Through the root, where cobra enforces the required flag; a bare RunE
	// no longer re-checks it.
	res := runMutationCLI(t, nil, 0, nil, "apps", "remove-owner", "app1")
	err := res.err
	if err == nil {
		t.Fatal("expected an error when --user-id is unset")
	}
	if res.exit != exitUsage {
		t.Errorf("exit = %d, want %d (usage)", res.exit, exitUsage)
	}
	if !strings.Contains(err.Error(), "user-id") {
		t.Errorf("expected error to mention user-id, got %v", err)
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

	_ = appsRemoveOwnerCmd.Flags().Set("user-id", "user1")

	var out bytes.Buffer
	appsRemoveOwnerCmd.SetOut(&out)
	appsRemoveOwnerCmd.SetContext(context.Background())

	if err := appsRemoveOwnerCmd.RunE(appsRemoveOwnerCmd, []string{"app1"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, `"method": "DELETE",`) || !strings.Contains(got, `"url": "https://example.invalid/api/v1/apps/app1/owners/user1"`) {
		t.Errorf("missing expected dry-run request, got %q", got)
	}
	if strings.Contains(got, `"body"`) {
		t.Errorf("remove-owner has no request body; dry-run should not print one, got %q", got)
	}
}
