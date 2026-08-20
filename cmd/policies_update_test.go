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

// stubPoliciesClient swaps newPoliciesClient to return a *client.Client
// wired (via client.NewForTesting) to a real httptest.Server, bypassing
// newClient's OAuth mint, restoring the original when the test ends. Mirrors
// cmd/requests_create_test.go's stubNewGrantClient.
func stubPoliciesClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	orig := newPoliciesClient
	newPoliciesClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newPoliciesClient = orig })
}

func withRealDryRun(t *testing.T) {
	t.Helper()
	orig := viper.GetBool("dry_run")
	viper.Set("dry_run", false)
	t.Cleanup(func() { viper.Set("dry_run", orig) })
}

// resetPoliciesUpdateCmdFlags restores policiesUpdateCmd's own flags to their
// zero values AND clears pflag's per-flag Changed bit, so tests sharing the
// package-level singleton command can't leak flag state into each other
// (mirrors api_test.go's resetAPICmdFlags). Using FlagSet.Set here (instead
// of the Value directly) would itself flip Changed to true, which would
// break every test that branches on cmd.Flags().Changed(...) — so this
// clears Changed explicitly after resetting the value.
func resetPoliciesUpdateCmdFlags(t *testing.T) {
	t.Helper()
	reset := func() {
		for _, name := range []string{"display-name", "description", "policy-type", "steps-file", "rules-file", "body-file", "update-mask"} {
			f := policiesUpdateCmd.Flags().Lookup(name)
			_ = f.Value.Set("")
			f.Changed = false
		}
		f := policiesUpdateCmd.Flags().Lookup("allow-deny-all")
		_ = f.Value.Set("false")
		f.Changed = false
	}
	reset()
	t.Cleanup(reset)
}

// TestPoliciesUpdateSendsWrappedBody is the load-bearing proof that "update"
// wraps the request as {"policy": {...}, "updateMask": "..."} — a flat body
// 400s server-side (protojson rejects the top-level keys as unknown fields
// on UpdatePolicyRequest). It drives policiesUpdateCmd.RunE directly against
// a real httptest.Server and asserts the exact bytes the server received,
// not just that the command returned no error.
func TestPoliciesUpdateSendsWrappedBody(t *testing.T) {
	const policyID = "zz-c1i-test-policy-1"

	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/policies/"+policyID {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Errorf("server: unmarshaling request body: %v", err)
		}
		_, _ = fmt.Fprint(w, `{"policy":{"id":"zz-c1i-test-policy-1","displayName":"new name"}}`)
	}))
	defer srv.Close()

	resetPoliciesUpdateCmdFlags(t)
	stubPoliciesClient(t, srv)
	withRealDryRun(t)
	t.Setenv("C1I_URL", srv.URL)

	_ = policiesUpdateCmd.Flags().Set("display-name", "new name")

	var out bytes.Buffer
	policiesUpdateCmd.SetOut(&out)
	policiesUpdateCmd.SetContext(context.Background())

	if err := policiesUpdateCmd.RunE(policiesUpdateCmd, []string{policyID}); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	// The two required top-level keys, and NOTHING flattened alongside them.
	if _, ok := gotBody["policy"]; !ok {
		t.Fatalf(`request body has no top-level "policy" key: %v`, gotBody)
	}
	if _, ok := gotBody["updateMask"]; !ok {
		t.Fatalf(`request body has no top-level "updateMask" key: %v`, gotBody)
	}
	if _, ok := gotBody["displayName"]; ok {
		t.Errorf(`"displayName" must be nested under "policy", not flattened to the top level: %v`, gotBody)
	}

	policy, ok := gotBody["policy"].(map[string]any)
	if !ok {
		t.Fatalf(`"policy" is not an object: %v`, gotBody["policy"])
	}
	if policy["displayName"] != "new name" {
		t.Errorf(`policy.displayName = %v, want "new name"`, policy["displayName"])
	}
	if policy["id"] != policyID {
		t.Errorf("policy.id = %v, want %q", policy["id"], policyID)
	}
	if gotBody["updateMask"] != "displayName" {
		t.Errorf(`updateMask = %v, want "displayName"`, gotBody["updateMask"])
	}
}

// TestPoliciesUpdateDryRunPreviewsWrappedBodyWithoutSending proves --dry-run
// shows the same wrapped shape and never hits the network.
func TestPoliciesUpdateDryRunPreviewsWrappedBodyWithoutSending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("dry-run must not send any request, got: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	resetPoliciesUpdateCmdFlags(t)
	stubPoliciesClient(t, srv)
	t.Setenv("C1I_URL", srv.URL)
	orig := viper.GetBool("dry_run")
	viper.Set("dry_run", true)
	t.Cleanup(func() { viper.Set("dry_run", orig) })

	_ = policiesUpdateCmd.Flags().Set("description", "new desc")

	var out bytes.Buffer
	policiesUpdateCmd.SetOut(&out)
	policiesUpdateCmd.SetContext(context.Background())

	if err := policiesUpdateCmd.RunE(policiesUpdateCmd, []string{"pol1"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if got := out.String(); !bytesContains(got, `"policy"`) || !bytesContains(got, `"updateMask"`) {
		t.Errorf("dry-run preview should show the wrapped body, got: %s", got)
	}
}

func bytesContains(s, sub string) bool {
	return bytes.Contains([]byte(s), []byte(sub))
}

// TestPoliciesUpdateGuardFiresOnEmptyStepsFile drives the guard through the
// real command (not just the pure validator) with a --steps-file containing
// an empty array, and confirms the server is never contacted.
func TestPoliciesUpdateGuardFiresOnEmptyStepsFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("guard should have refused before any request was sent, got: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	resetPoliciesUpdateCmdFlags(t)
	stubPoliciesClient(t, srv)
	withRealDryRun(t)
	t.Setenv("C1I_URL", srv.URL)

	stepsPath := writeTempJSON(t, "steps.json", `[]`)
	_ = policiesUpdateCmd.Flags().Set("policy-type", "grant")
	_ = policiesUpdateCmd.Flags().Set("steps-file", stepsPath)

	var out bytes.Buffer
	policiesUpdateCmd.SetOut(&out)
	policiesUpdateCmd.SetContext(context.Background())

	err := policiesUpdateCmd.RunE(policiesUpdateCmd, []string{"pol1"})
	if err == nil {
		t.Fatal("expected the guard to refuse an empty --steps-file array")
	}
	if exitCode(err) != exitUsage {
		t.Errorf("exitCode(%v) = %d, want %d (exitUsage)", err, exitCode(err), exitUsage)
	}
}

// TestPoliciesUpdateAllowDenyAllBypassesGuardAndSendsRequest proves the
// deny-all opt-in actually reaches the server: with no --steps-file and
// --allow-deny-all set, "policyType" isn't even part of the patch (nothing
// else changed either), so this uses --description alongside --allow-deny-all
// on a from-scratch policySteps-omitting flow via --body-file to exercise
// the bypass end to end.
func TestPoliciesUpdateAllowDenyAllBypassesGuardAndSendsRequest(t *testing.T) {
	const policyID = "zz-c1i-test-policy-2"
	var requestReceived bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestReceived = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"policy":{"id":"zz-c1i-test-policy-2"}}`)
	}))
	defer srv.Close()

	resetPoliciesUpdateCmdFlags(t)
	stubPoliciesClient(t, srv)
	withRealDryRun(t)
	t.Setenv("C1I_URL", srv.URL)

	bodyPath := writeTempJSON(t, "body.json", `{"policyType":"POLICY_TYPE_GRANT"}`)
	_ = policiesUpdateCmd.Flags().Set("body-file", bodyPath)
	_ = policiesUpdateCmd.Flags().Set("update-mask", "policySteps")
	_ = policiesUpdateCmd.Flags().Set("allow-deny-all", "true")

	var out bytes.Buffer
	policiesUpdateCmd.SetOut(&out)
	policiesUpdateCmd.SetContext(context.Background())

	if err := policiesUpdateCmd.RunE(policiesUpdateCmd, []string{policyID}); err != nil {
		t.Fatalf("RunE: %v (expected --allow-deny-all to permit this and send the request)", err)
	}
	if !requestReceived {
		t.Error("expected the request to actually reach the server when --allow-deny-all is set")
	}
}

// TestPoliciesUpdateBodyFileRequiresUpdateMask pins that --body-file without
// --update-mask is refused (there would be nothing to tell the server what
// changed).
func TestPoliciesUpdateBodyFileRequiresUpdateMask(t *testing.T) {
	resetPoliciesUpdateCmdFlags(t)
	bodyPath := writeTempJSON(t, "body.json", `{"displayName":"x"}`)
	cmd := newPoliciesCreateFlagCmd(t) // reuse: same flag names needed here
	cmd.Flags().String("update-mask", "", "")
	_ = cmd.Flags().Set("body-file", bodyPath)

	_, _, err := buildUpdatePolicyPatch(cmd, func() (string, error) { return "", nil })
	if err == nil {
		t.Fatal("expected an error: --body-file requires --update-mask")
	}
}
