package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// TestBuildGrantTaskBodyNoWrapper pins the fix for the 400 "unknown field
// \"task\"" bug: POST /api/v1/task/grant (CreateGrantTaskRequest) expects its
// fields at the top level, never nested under a "task" key. It also pins the
// proto field names — identityUserId (not userId) and grantDuration (not
// duration) — which would otherwise be rejected as unknown fields too.
func TestBuildGrantTaskBodyNoWrapper(t *testing.T) {
	body := buildGrantTaskBody("app1", "ent1", "user1", "24h", "test", true)

	if _, wrapped := body["task"]; wrapped {
		t.Fatalf("body must not wrap fields under a \"task\" key: %v", body)
	}

	// Re-marshal to inspect the exact wire shape the server sees.
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	want := map[string]any{
		"appId":            "app1",
		"appEntitlementId": "ent1",
		"identityUserId":   "user1",
		"grantDuration":    "24h",
		"description":      "test",
		"emergencyAccess":  true,
	}
	for k, v := range want {
		if wire[k] != v {
			t.Errorf("wire[%q] = %v, want %v", k, wire[k], v)
		}
	}
	// No stray/legacy field names should leak through.
	for _, bad := range []string{"task", "userId", "duration"} {
		if _, ok := wire[bad]; ok {
			t.Errorf("unexpected field %q in wire body: %v", bad, wire)
		}
	}
	// Pin the exact wire shape: only the fields in `want`, nothing else.
	if len(wire) != len(want) {
		t.Errorf("wire body has %d fields, want exactly %d: %v", len(wire), len(want), wire)
	}
}

// TestBuildGrantTaskBodyOmitsEmpty confirms optional fields are omitted when
// unset, so the request carries only appId/appEntitlementId (self-request,
// default duration, no description, non-emergency).
func TestBuildGrantTaskBodyOmitsEmpty(t *testing.T) {
	body := buildGrantTaskBody("app1", "ent1", "", "", "", false)
	if len(body) != 2 {
		t.Fatalf("expected only appId and appEntitlementId, got %v", body)
	}
	if body["appId"] != "app1" || body["appEntitlementId"] != "ent1" {
		t.Errorf("unexpected body: %v", body)
	}
}

// resetGrantCmdFlags restores requestsCreateGrantCmd's own flags to their
// zero values, so tests sharing the package-level singleton can't leak flag
// state into each other or into other test files (mirrors api_test.go's
// resetAPICmdFlags).
func resetGrantCmdFlags(t *testing.T) {
	t.Helper()
	reset := func() {
		_ = requestsCreateGrantCmd.Flags().Set("app-id", "")
		_ = requestsCreateGrantCmd.Flags().Set("entitlement-id", "")
		_ = requestsCreateGrantCmd.Flags().Set("user-id", "")
		_ = requestsCreateGrantCmd.Flags().Set("duration", "")
		_ = requestsCreateGrantCmd.Flags().Set("description", "")
		_ = requestsCreateGrantCmd.Flags().Set("emergency", "false")
	}
	reset()
	t.Cleanup(reset)
}

// stubNewGrantClient swaps newGrantClient to return a *client.Client wired
// (via client.NewForTesting) to a real httptest.Server, bypassing newClient's
// OAuth mint, restoring the original when the test ends.
func stubNewGrantClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	orig := newGrantClient
	newGrantClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newGrantClient = orig })
}

// TestRequestsCreateGrantDefaultsUserIDToSelf pins the fix for --user-id's
// help text ("defaults to self if omitted") not actually having a default:
// omitting it used to send no identityUserId at all, which the API rejects
// with a 500 "user_id is required". It now resolves the caller's own id via
// the same introspect-based currentUserID lookup requests_list.go's default
// requester scope and tasks_list.go's --assigned-to-me already use.
//
// It drives requestsCreateGrantCmd.RunE directly (not through rootCmd/cobra's
// full parse-and-execute path — GetBaseURL/dryRunActive read viper directly,
// so this doesn't need it) against a real httptest.Server standing in for
// both /api/v1/auth/introspect and /api/v1/task/grant, and asserts the server
// actually received the resolved id in the POST body — not just that the
// command returned no error.
func TestRequestsCreateGrantDefaultsUserIDToSelf(t *testing.T) {
	const selfUserID = "zz-c1i-test-self-user-id"

	var gotIntrospect bool
	var gotGrantBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/auth/introspect":
			gotIntrospect = true
			_, _ = fmt.Fprintf(w, `{"userId":%q}`, selfUserID)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/task/grant":
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &gotGrantBody); err != nil {
				t.Errorf("server: unmarshaling request body: %v", err)
			}
			_, _ = fmt.Fprint(w, `{"taskView":{"task":{"id":"task-1","state":"PENDING"}}}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	resetGrantCmdFlags(t)
	stubNewGrantClient(t, srv)
	t.Setenv("C1I_URL", srv.URL)

	origDryRun := viper.GetBool("dry_run")
	viper.Set("dry_run", false)
	t.Cleanup(func() { viper.Set("dry_run", origDryRun) })

	_ = requestsCreateGrantCmd.Flags().Set("app-id", "app1")
	_ = requestsCreateGrantCmd.Flags().Set("entitlement-id", "ent1")
	// --user-id intentionally left unset.

	var out bytes.Buffer
	requestsCreateGrantCmd.SetOut(&out)
	requestsCreateGrantCmd.SetContext(context.Background())

	if err := requestsCreateGrantCmd.RunE(requestsCreateGrantCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if !gotIntrospect {
		t.Error("expected the command to resolve self via /api/v1/auth/introspect")
	}
	if got := gotGrantBody["identityUserId"]; got != selfUserID {
		t.Errorf("server received identityUserId = %v, want %q", got, selfUserID)
	}
}

// TestRequestsCreateGrantExplicitUserIDSkipsSelfResolution is a regression
// guard alongside the default-to-self test: an explicit --user-id must be
// sent as-is, without ever calling introspect.
func TestRequestsCreateGrantExplicitUserIDSkipsSelfResolution(t *testing.T) {
	const explicitUserID = "zz-c1i-test-explicit-user-id"

	var gotIntrospect bool
	var gotGrantBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/auth/introspect":
			gotIntrospect = true
			_, _ = fmt.Fprint(w, `{"userId":"should-not-be-used"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/task/grant":
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &gotGrantBody); err != nil {
				t.Errorf("server: unmarshaling request body: %v", err)
			}
			_, _ = fmt.Fprint(w, `{"taskView":{"task":{"id":"task-1","state":"PENDING"}}}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	resetGrantCmdFlags(t)
	stubNewGrantClient(t, srv)
	t.Setenv("C1I_URL", srv.URL)

	origDryRun := viper.GetBool("dry_run")
	viper.Set("dry_run", false)
	t.Cleanup(func() { viper.Set("dry_run", origDryRun) })

	_ = requestsCreateGrantCmd.Flags().Set("app-id", "app1")
	_ = requestsCreateGrantCmd.Flags().Set("entitlement-id", "ent1")
	_ = requestsCreateGrantCmd.Flags().Set("user-id", explicitUserID)

	var out bytes.Buffer
	requestsCreateGrantCmd.SetOut(&out)
	requestsCreateGrantCmd.SetContext(context.Background())

	if err := requestsCreateGrantCmd.RunE(requestsCreateGrantCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if gotIntrospect {
		t.Error("expected introspect NOT to be called when --user-id is explicit")
	}
	if got := gotGrantBody["identityUserId"]; got != explicitUserID {
		t.Errorf("server received identityUserId = %v, want %q", got, explicitUserID)
	}
}

// TestBuildRevokeTaskBodyNoWrapper pins the same flat-body contract for
// POST /api/v1/task/revoke (CreateRevokeTaskRequest).
func TestBuildRevokeTaskBodyNoWrapper(t *testing.T) {
	body := buildRevokeTaskBody("app1", "ent1", "user1", "test")

	if _, wrapped := body["task"]; wrapped {
		t.Fatalf("body must not wrap fields under a \"task\" key: %v", body)
	}

	want := map[string]any{
		"appId":            "app1",
		"appEntitlementId": "ent1",
		"identityUserId":   "user1",
		"description":      "test",
	}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("body[%q] = %v, want %v", k, body[k], v)
		}
	}
	if _, ok := body["userId"]; ok {
		t.Errorf("unexpected legacy field \"userId\" in body: %v", body)
	}
	// Pin the exact wire shape: only the fields in `want`, nothing else.
	if len(body) != len(want) {
		t.Errorf("body has %d fields, want exactly %d: %v", len(body), len(want), body)
	}
}

// TestBuildRevokeTaskBodyOmitsEmpty mirrors the grant omit-empty case: with no
// user or description, the body carries only appId/appEntitlementId.
func TestBuildRevokeTaskBodyOmitsEmpty(t *testing.T) {
	body := buildRevokeTaskBody("app1", "ent1", "", "")
	if len(body) != 2 {
		t.Fatalf("expected only appId and appEntitlementId, got %v", body)
	}
	if body["appId"] != "app1" || body["appEntitlementId"] != "ent1" {
		t.Errorf("unexpected body: %v", body)
	}
}

// resetRevokeCmdFlags mirrors resetGrantCmdFlags for requestsCreateRevokeCmd's
// own flags.
func resetRevokeCmdFlags(t *testing.T) {
	t.Helper()
	reset := func() {
		_ = requestsCreateRevokeCmd.Flags().Set("app-id", "")
		_ = requestsCreateRevokeCmd.Flags().Set("entitlement-id", "")
		_ = requestsCreateRevokeCmd.Flags().Set("user-id", "")
		_ = requestsCreateRevokeCmd.Flags().Set("description", "")
	}
	reset()
	t.Cleanup(reset)
}

// stubNewRevokeClient mirrors stubNewGrantClient for newRevokeClient.
func stubNewRevokeClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	orig := newRevokeClient
	newRevokeClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newRevokeClient = orig })
}

// TestRequestsCreateRevokeDefaultsUserIDToSelf mirrors
// TestRequestsCreateGrantDefaultsUserIDToSelf for the identical twin defect in
// `requests create revoke`: --user-id's help also promises "defaults to self
// if omitted" and also sent no identityUserId at all, hitting the same API
// 500 "user_id is required". Same fix, same currentUserID lookup, same proof
// shape: assert against a real httptest.Server that the server received the
// resolved id, not just that the command returned no error.
func TestRequestsCreateRevokeDefaultsUserIDToSelf(t *testing.T) {
	const selfUserID = "zz-c1i-test-self-user-id"

	var gotIntrospect bool
	var gotRevokeBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/auth/introspect":
			gotIntrospect = true
			_, _ = fmt.Fprintf(w, `{"userId":%q}`, selfUserID)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/task/revoke":
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &gotRevokeBody); err != nil {
				t.Errorf("server: unmarshaling request body: %v", err)
			}
			_, _ = fmt.Fprint(w, `{"taskView":{"task":{"id":"task-1","state":"PENDING"}}}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	resetRevokeCmdFlags(t)
	stubNewRevokeClient(t, srv)
	t.Setenv("C1I_URL", srv.URL)

	origDryRun := viper.GetBool("dry_run")
	viper.Set("dry_run", false)
	t.Cleanup(func() { viper.Set("dry_run", origDryRun) })

	_ = requestsCreateRevokeCmd.Flags().Set("app-id", "app1")
	_ = requestsCreateRevokeCmd.Flags().Set("entitlement-id", "ent1")
	// --user-id intentionally left unset.

	var out bytes.Buffer
	requestsCreateRevokeCmd.SetOut(&out)
	requestsCreateRevokeCmd.SetContext(context.Background())

	if err := requestsCreateRevokeCmd.RunE(requestsCreateRevokeCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if !gotIntrospect {
		t.Error("expected the command to resolve self via /api/v1/auth/introspect")
	}
	if got := gotRevokeBody["identityUserId"]; got != selfUserID {
		t.Errorf("server received identityUserId = %v, want %q", got, selfUserID)
	}
}

// TestRequestsCreateRevokeExplicitUserIDSkipsSelfResolution mirrors the grant
// regression guard: an explicit --user-id must be sent as-is, without ever
// calling introspect.
func TestRequestsCreateRevokeExplicitUserIDSkipsSelfResolution(t *testing.T) {
	const explicitUserID = "zz-c1i-test-explicit-user-id"

	var gotIntrospect bool
	var gotRevokeBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/auth/introspect":
			gotIntrospect = true
			_, _ = fmt.Fprint(w, `{"userId":"should-not-be-used"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/task/revoke":
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &gotRevokeBody); err != nil {
				t.Errorf("server: unmarshaling request body: %v", err)
			}
			_, _ = fmt.Fprint(w, `{"taskView":{"task":{"id":"task-1","state":"PENDING"}}}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	resetRevokeCmdFlags(t)
	stubNewRevokeClient(t, srv)
	t.Setenv("C1I_URL", srv.URL)

	origDryRun := viper.GetBool("dry_run")
	viper.Set("dry_run", false)
	t.Cleanup(func() { viper.Set("dry_run", origDryRun) })

	_ = requestsCreateRevokeCmd.Flags().Set("app-id", "app1")
	_ = requestsCreateRevokeCmd.Flags().Set("entitlement-id", "ent1")
	_ = requestsCreateRevokeCmd.Flags().Set("user-id", explicitUserID)

	var out bytes.Buffer
	requestsCreateRevokeCmd.SetOut(&out)
	requestsCreateRevokeCmd.SetContext(context.Background())

	if err := requestsCreateRevokeCmd.RunE(requestsCreateRevokeCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if gotIntrospect {
		t.Error("expected introspect NOT to be called when --user-id is explicit")
	}
	if got := gotRevokeBody["identityUserId"]; got != explicitUserID {
		t.Errorf("server received identityUserId = %v, want %q", got, explicitUserID)
	}
}
