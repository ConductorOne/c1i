package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/ConductorOne/c1i/internal/client"
)

// accountsGetServer answers POST /api/v1/search/app_users with the list the
// test supplies, verifying the request body filters by appId/appUserIds.
func accountsGetServer(t *testing.T, wantAppID, wantAppUserID, listJSON string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/search/app_users" {
			t.Errorf("unexpected request: %s %s (want POST /api/v1/search/app_users)", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		if body["appId"] != wantAppID {
			t.Errorf("appId = %v, want %q", body["appId"], wantAppID)
		}
		ids, _ := body["appUserIds"].([]any)
		if len(ids) != 1 || ids[0] != wantAppUserID {
			t.Errorf("appUserIds = %v, want [%q]", body["appUserIds"], wantAppUserID)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, listJSON)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// runAccountsGet resets accountsGetCmd's flags, points newClient at srv, and
// runs it with the given positional id and --app-id.
func runAccountsGet(t *testing.T, srv *httptest.Server, appUserID, appID string) (string, error) {
	t.Helper()
	resetCmdFlags(t, accountsGetCmd)
	orig := newClient
	newClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newClient = orig })
	t.Setenv("C1I_URL", "https://example.invalid")
	if err := accountsGetCmd.Flags().Set("app-id", appID); err != nil {
		t.Fatalf("setting --app-id: %v", err)
	}
	var out bytes.Buffer
	restoreCmdWriters(t)
	accountsGetCmd.SetOut(&out)
	accountsGetCmd.SetContext(context.Background())
	t.Cleanup(func() {
		accountsGetCmd.SetOut(nil)
		accountsGetCmd.SetContext(context.Background())
	})
	err := accountsGetCmd.RunE(accountsGetCmd, []string{appUserID})
	return out.String(), err
}

// A single match prints the appUser object unwrapped, profile included --
// the field "accounts list" rows drop.
func TestAccountsGetPrintsUnwrappedAppUserWithProfile(t *testing.T) {
	srv := accountsGetServer(t, "app1", "au1", `{"list":[{"appUser":{"id":"au1","appId":"app1","displayName":"Ada","profile":{"department":"Eng"}}}]}`)
	out, err := runAccountsGet(t, srv, "au1", "app1")
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output not JSON: %v (%s)", err, out)
	}
	if got["id"] != "au1" {
		t.Errorf("id = %v, want au1", got["id"])
	}
	profile, _ := got["profile"].(map[string]any)
	if profile["department"] != "Eng" {
		t.Errorf("profile.department = %v, want Eng (profile should be unwrapped, not dropped like a list row)", got["profile"])
	}
}

// --fields projects the single-object output the same way other gets do.
func TestAccountsGetFieldsProjectsProfile(t *testing.T) {
	srv := accountsGetServer(t, "app1", "au1", `{"list":[{"appUser":{"id":"au1","appId":"app1","profile":{"department":"Eng"}}}]}`)
	clearViperOverrides(t, "fields")
	viper.Set("fields", "profile.department")
	t.Cleanup(func() { clearViperOverrides(t, "fields") })
	out, err := runAccountsGet(t, srv, "au1", "app1")
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output not JSON: %v (%s)", err, out)
	}
	profile, _ := got["profile"].(map[string]any)
	if profile["department"] != "Eng" {
		t.Errorf("projected output = %v, want {\"profile\":{\"department\":\"Eng\"}}", got)
	}
}

// Zero matches (the id doesn't exist, or exists under a different app) exits
// 4, like refNotFoundError everywhere else.
func TestAccountsGetNoMatchExitsNotFound(t *testing.T) {
	srv := accountsGetServer(t, "app1", "missing", `{"list":[]}`)
	_, err := runAccountsGet(t, srv, "missing", "app1")
	if err == nil {
		t.Fatal("RunE: want an error for zero matches")
	}
	if exitCode(err) != exitNotFound {
		t.Errorf("exitCode = %d, want %d (exitNotFound): %v", exitCode(err), exitNotFound, err)
	}
}

// A result list whose appUser.id never equals the requested id (e.g. the
// filter returned unrelated rows) is treated the same as zero matches.
func TestAccountsGetNoExactMatchExitsNotFound(t *testing.T) {
	srv := accountsGetServer(t, "app1", "au1", `{"list":[{"appUser":{"id":"other-id","appId":"app1"}}]}`)
	_, err := runAccountsGet(t, srv, "au1", "app1")
	if err == nil {
		t.Fatal("RunE: want an error when no row's id matches the requested id")
	}
	if exitCode(err) != exitNotFound {
		t.Errorf("exitCode = %d, want %d (exitNotFound): %v", exitCode(err), exitNotFound, err)
	}
}

// More than one row claiming the requested id is a C1 failure, not a usage
// or not-found error: the server broke the single-id-filter contract.
func TestAccountsGetMultipleMatchesExitsServerError(t *testing.T) {
	srv := accountsGetServer(t, "app1", "au1", `{"list":[{"appUser":{"id":"au1","appId":"app1"}},{"appUser":{"id":"au1","appId":"app1"}}]}`)
	_, err := runAccountsGet(t, srv, "au1", "app1")
	if err == nil {
		t.Fatal("RunE: want an error for more than one match")
	}
	if exitCode(err) != exitServer {
		t.Errorf("exitCode = %d, want %d (exitServer): %v", exitCode(err), exitServer, err)
	}
}
