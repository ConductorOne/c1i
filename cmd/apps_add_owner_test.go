package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// TestAppsAddOwnerRequiresUserID proves a missing --user-id is rejected
// as a usage error (exit 2) before any network call is attempted -- no
// C1I_URL is set here, so if the command reached newClient it would fail on
// URL resolution instead, which would misreport as a different failure mode.
func TestAppsAddOwnerRequiresUserID(t *testing.T) {
	// Through the root, where cobra enforces the required flag; a bare RunE
	// no longer re-checks it.
	res := runMutationCLI(t, nil, 0, nil, "apps", "add-owner", "app1")
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

// TestAppsAddOwnerDryRunPreviewsRequestWithoutSending proves --dry-run
// prints the exact POST, path and body the real call would send -- `{}`,
// which the endpoint parses as a protobuf message -- without touching the
// network. A preview that omitted the body would misrepresent the request.
func TestAppsAddOwnerDryRunPreviewsRequestWithoutSending(t *testing.T) {
	resetCmdFlags(t, appsAddOwnerCmd)
	t.Setenv("C1I_URL", "https://example.invalid")
	// The preview looks the user up (a read), so the stub must answer it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/users/user1" {
			t.Errorf("unexpected request under --dry-run: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"user":{"id":"user1"}}`))
	}))
	defer srv.Close()
	stubNewGrantClient(t, srv)

	clearViperOverrides(t, "dry_run")
	viper.Set("dry_run", true)

	_ = appsAddOwnerCmd.Flags().Set("user-id", "user1")

	var out bytes.Buffer
	restoreCmdWriters(t)
	appsAddOwnerCmd.SetOut(&out)
	appsAddOwnerCmd.SetContext(context.Background())

	if err := appsAddOwnerCmd.RunE(appsAddOwnerCmd, []string{"app1"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	got := out.String()
	// The preview must match the wire: the endpoint parses the body as a
	// protobuf message, so the command sends `{}` and a dry-run that showed
	// nothing would misrepresent the request it is previewing.
	want := "{\n  \"dryRun\": true,\n  \"requests\": [\n    {\n      \"method\": \"POST\",\n      \"url\": \"https://example.invalid/api/v1/apps/app1/owners/user1\",\n      \"body\": {}\n    }\n  ]\n}\n"
	if got != want {
		t.Errorf("dry-run preview = %q, want %q", got, want)
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
