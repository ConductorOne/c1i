package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// TestAppsAddOwnerRequiresAppID proves a missing/empty --app-id is rejected
// as a usage error (exit 2) before any network call is attempted -- no
// C1I_URL is set here, so if the command reached newClient it would fail on
// URL resolution instead, which would misreport as a different failure mode.
func TestAppsAddOwnerRequiresAppID(t *testing.T) {
	resetCmdFlags(t, appsAddOwnerCmd)

	var out bytes.Buffer
	appsAddOwnerCmd.SetOut(&out)
	appsAddOwnerCmd.SetContext(context.Background())

	err := appsAddOwnerCmd.RunE(appsAddOwnerCmd, []string{"user1"})
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

// TestAppsAddOwnerDryRunPreviewsRequestWithoutSending proves --dry-run
// prints the exact POST, path and body the real call would send -- `{}`,
// which the endpoint parses as a protobuf message -- without touching the
// network. A preview that omitted the body would misrepresent the request.
func TestAppsAddOwnerDryRunPreviewsRequestWithoutSending(t *testing.T) {
	resetCmdFlags(t, appsAddOwnerCmd)
	t.Setenv("C1I_URL", "https://example.invalid")

	orig := viper.GetBool("dry_run")
	viper.Set("dry_run", true)
	t.Cleanup(func() { viper.Set("dry_run", orig) })

	_ = appsAddOwnerCmd.Flags().Set("app-id", "app1")

	var out bytes.Buffer
	appsAddOwnerCmd.SetOut(&out)
	appsAddOwnerCmd.SetContext(context.Background())

	if err := appsAddOwnerCmd.RunE(appsAddOwnerCmd, []string{"user1"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "[dry-run] POST /api/v1/apps/app1/owners/user1") {
		t.Errorf("missing expected dry-run request line, got %q", got)
	}
	// The preview must match the wire: the endpoint parses the body as a
	// protobuf message, so the command sends `{}` and a dry-run that showed
	// nothing would misrepresent the request it is previewing.
	if !strings.Contains(got, "{}") {
		t.Errorf("dry-run should preview the %s body the request actually sends, got %q", "{}", got)
	}
}

// TestAppsAddOwnerSendsEmptyObjectNotNull pins the body shape. The endpoint
// parses the body as a protobuf message even though it needs nothing from it,
// so a nil body (which json.Marshal renders as `null`) is rejected live with
// 400 "failed to unmarshal body: proto: syntax error ... unexpected token
// null". Unit tests never see that, because the mutation path does no HTTP --
// this pins the one value that keeps it from happening again.
func TestAppsAddOwnerSendsEmptyObjectNotNull(t *testing.T) {
	got, err := json.Marshal(addOwnerEmptyBody())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(got) != "{}" {
		t.Errorf("add-owner request body = %s, want {} (a nil body marshals to null and the API 400s)", got)
	}
}
