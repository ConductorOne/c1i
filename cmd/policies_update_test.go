package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	t.Setenv("C1I_URL", "https://example.invalid")

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
	t.Setenv("C1I_URL", "https://example.invalid")
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
	t.Setenv("C1I_URL", "https://example.invalid")

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
	t.Setenv("C1I_URL", "https://example.invalid")

	// The body must actually TRIP the guard, or this proves nothing about the
	// bypass: policySteps is present (so the steps guard runs) but the
	// baseline "grant" key is absent, which is exactly the silent deny-all
	// case --allow-deny-all exists to permit.
	bodyPath := writeTempJSON(t, "body.json",
		`{"policyType":"POLICY_TYPE_GRANT","policySteps":{"someOtherKey":{"steps":[{"accept":{}}]}}}`)
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

// TestPoliciesUpdateWithoutAllowDenyAllIsRefused is the other half of the
// bypass pair above. A bypass test on its own proves little: it asserts the
// request reaches the server, which also happens if the guard is simply
// broken. Only this negative — the SAME payload refused when the flag is
// absent — shows the flag is what permits it.
func TestPoliciesUpdateWithoutAllowDenyAllIsRefused(t *testing.T) {
	const policyID = "zz-c1i-test-policy-3"
	var requestReceived bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestReceived = true
	}))
	defer srv.Close()

	resetPoliciesUpdateCmdFlags(t)
	stubPoliciesClient(t, srv)
	withRealDryRun(t)
	t.Setenv("C1I_URL", "https://example.invalid")

	bodyPath := writeTempJSON(t, "body.json",
		`{"policyType":"POLICY_TYPE_GRANT","policySteps":{"someOtherKey":{"steps":[{"accept":{}}]}}}`)
	_ = policiesUpdateCmd.Flags().Set("body-file", bodyPath)
	_ = policiesUpdateCmd.Flags().Set("update-mask", "policySteps")
	// deliberately NOT setting --allow-deny-all

	var out bytes.Buffer
	policiesUpdateCmd.SetOut(&out)
	policiesUpdateCmd.SetContext(context.Background())

	err := policiesUpdateCmd.RunE(policiesUpdateCmd, []string{policyID})
	if err == nil {
		t.Fatal("expected the missing-baseline guard to refuse this without --allow-deny-all")
	}
	if exitCode(err) != exitUsage {
		t.Errorf("exitCode(%v) = %d, want %d (a guard rejection is a usage error)", err, exitCode(err), exitUsage)
	}
	if requestReceived {
		t.Error("guard rejected the input but a request still reached the server")
	}
}

// TestPoliciesUpdateBodyFileRequiresUpdateMask pins that --body-file without
// --update-mask is refused: nothing would tell the server what changed.
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

// TestPoliciesUpdateBodyFileResolvesPolicyTypeForAgentStepGuard is the
// load-bearing regression test for the --body-file guard bypass: a patch
// carrying an agent approval step but no top-level "policyType" must still
// be checked against the policy's REAL type (fetched from the server), not
// silently validated with policyType=="" (which skips the "agent steps only
// in grant/certify" rule entirely and lets the request reach the server,
// where it 500s). The stub server's POST handler would report success if
// reached — requestReceived catches that instead of relying on the POST
// handler failing the test, so this test fails clearly rather than by
// accident if the guard regresses.
func TestPoliciesUpdateBodyFileResolvesPolicyTypeForAgentStepGuard(t *testing.T) {
	const policyID = "zz-c1i-test-policy-6"
	var getReceived, postReceived bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/policies/"+policyID:
			getReceived = true
			_, _ = fmt.Fprint(w, `{"policy":{"id":"zz-c1i-test-policy-6","policyType":"POLICY_TYPE_REVOKE"}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/policies/"+policyID:
			postReceived = true
			_, _ = fmt.Fprint(w, `{"policy":{"id":"zz-c1i-test-policy-6"}}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	resetPoliciesUpdateCmdFlags(t)
	stubPoliciesClient(t, srv)
	withRealDryRun(t)
	t.Setenv("C1I_URL", "https://example.invalid")

	// No top-level "policyType" — the omission this defect is about. Agent
	// approval steps are only allowed in grant/certify policies; the
	// server-side policy is REVOKE, so this must be refused once the real
	// type is known.
	bodyPath := writeTempJSON(t, "body.json", `{"policySteps":{"revoke":{"steps":[`+
		`{"approval":{"agent":{"agentMode":"APPROVAL_AGENT_MODE_COMMENT_ONLY",`+
		`"agentFailureAction":"APPROVAL_AGENT_FAILURE_ACTION_SKIP_POLICY_STEP"}}}`+
		`]}}}`)
	_ = policiesUpdateCmd.Flags().Set("body-file", bodyPath)
	_ = policiesUpdateCmd.Flags().Set("update-mask", "policySteps")

	var out bytes.Buffer
	policiesUpdateCmd.SetOut(&out)
	policiesUpdateCmd.SetContext(context.Background())

	err := policiesUpdateCmd.RunE(policiesUpdateCmd, []string{policyID})
	if err == nil {
		t.Fatal("expected the agent-steps-only-in-grant-or-certify guard to refuse this REVOKE-type update sent via --body-file")
	}
	if exitCode(err) != exitUsage {
		t.Errorf("exitCode(%v) = %d, want %d (exitUsage) — a guard rejection, not a server round trip", err, exitCode(err), exitUsage)
	}
	if !getReceived {
		t.Error("expected the command to fetch the policy's current type since policyType was omitted from a policySteps-carrying patch")
	}
	if postReceived {
		t.Error("the guard should have refused before the update POST ever reached the server")
	}
}

// TestPoliciesUpdateBodyFileWithoutPolicyStepsNeverFetchesPolicyType is the
// negative half of the fix above: a --body-file patch that doesn't touch
// policySteps at all must never pay for the policyType lookup.
func TestPoliciesUpdateBodyFileWithoutPolicyStepsNeverFetchesPolicyType(t *testing.T) {
	const policyID = "zz-c1i-test-policy-7"
	var getReceived bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			getReceived = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"policy":{"id":"zz-c1i-test-policy-7"}}`)
	}))
	defer srv.Close()

	resetPoliciesUpdateCmdFlags(t)
	stubPoliciesClient(t, srv)
	withRealDryRun(t)
	t.Setenv("C1I_URL", "https://example.invalid")

	bodyPath := writeTempJSON(t, "body.json", `{"displayName":"new name"}`)
	_ = policiesUpdateCmd.Flags().Set("body-file", bodyPath)
	_ = policiesUpdateCmd.Flags().Set("update-mask", "displayName")

	var out bytes.Buffer
	policiesUpdateCmd.SetOut(&out)
	policiesUpdateCmd.SetContext(context.Background())

	if err := policiesUpdateCmd.RunE(policiesUpdateCmd, []string{policyID}); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if getReceived {
		t.Error("a --body-file update with no policySteps should never fetch the policy's current type")
	}
}

// newResolverTestCmd builds a bare *cobra.Command with a context, enough for
// policyTypeResolver.resolve (which only needs cmd.Context() and, via
// newPoliciesClient, cmd itself — stubbed below to ignore it).
func newResolverTestCmd(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "test"}
	cmd.SetContext(context.Background())
	return cmd
}

// TestPolicyTypeResolverMemoizesFetchFailure is the load-bearing regression
// test for the fix: a fetch failure (404 here) must be memoized alongside
// the (empty) value, so a second resolve() call sees the SAME classified
// error instead of a nil error with an empty type — which would look like
// "resolved to nothing" and let a caller downgrade a real 404 to exit 2.
// Asserts: both calls return the identical error, errors.As still finds a
// *client.APIError carrying 404 (so exitCode still maps it to 4, not 2), and
// the server receives exactly one request (proving no re-fetch).
func TestPolicyTypeResolverMemoizesFetchFailure(t *testing.T) {
	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"message":"policy not found"}`)
	}))
	defer srv.Close()
	stubPoliciesClient(t, srv)

	resolver := &policyTypeResolver{baseURL: srv.URL}
	cmd := newResolverTestCmd(t)

	val1, err1 := resolver.resolve(cmd, "zz-does-not-exist")
	if err1 == nil {
		t.Fatal("expected the first resolve() to return the 404 as an error")
	}
	if val1 != "" {
		t.Errorf("value on a failed fetch = %q, want empty", val1)
	}

	val2, err2 := resolver.resolve(cmd, "zz-does-not-exist")
	if err2 != err1 {
		t.Errorf("second resolve() returned a different error: %v (first: %v)", err2, err1)
	}
	if val2 != "" {
		t.Errorf("second resolve() value = %q, want empty", val2)
	}

	var apiErr *client.APIError
	if !errors.As(err2, &apiErr) {
		t.Fatalf("errors.As(%v, *client.APIError) = false, want true", err2)
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("apiErr.StatusCode = %d, want %d", apiErr.StatusCode, http.StatusNotFound)
	}
	if exitCode(err2) != exitNotFound {
		t.Errorf("exitCode(%v) = %d, want %d (exitNotFound) — a memoized 404 must not downgrade to exitUsage", err2, exitCode(err2), exitNotFound)
	}

	if requestCount != 1 {
		t.Errorf("server received %d requests, want exactly 1 (no re-fetch on the second resolve() call)", requestCount)
	}
}

// TestPolicyTypeResolverMemoizesSuccess is the positive half: two resolve()
// calls after a successful fetch return the same value, and the server is
// only hit once.
func TestPolicyTypeResolverMemoizesSuccess(t *testing.T) {
	var requestCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"policy":{"id":"zz-c1i-test-policy-resolver","policyType":"POLICY_TYPE_GRANT"}}`)
	}))
	defer srv.Close()
	stubPoliciesClient(t, srv)

	resolver := &policyTypeResolver{baseURL: srv.URL}
	cmd := newResolverTestCmd(t)

	val1, err1 := resolver.resolve(cmd, "zz-c1i-test-policy-resolver")
	if err1 != nil {
		t.Fatalf("first resolve(): %v", err1)
	}
	if val1 != "POLICY_TYPE_GRANT" {
		t.Fatalf("first resolve() value = %q, want POLICY_TYPE_GRANT", val1)
	}

	val2, err2 := resolver.resolve(cmd, "zz-c1i-test-policy-resolver")
	if err2 != nil {
		t.Fatalf("second resolve(): %v", err2)
	}
	if val2 != val1 {
		t.Errorf("second resolve() value = %q, want %q (same as first)", val2, val1)
	}

	if requestCount != 1 {
		t.Errorf("server received %d requests, want exactly 1 (no re-fetch on the second resolve() call)", requestCount)
	}
}

// TestPolicyTypeResolverMemoizesClientConstructionFailure covers the other
// failure point: newPoliciesClient itself (not the fetch) failing, e.g. bad
// credentials. A second resolve() call must not retry building the client
// either — it counts calls to newPoliciesClient directly since this failure
// never reaches a server.
func TestPolicyTypeResolverMemoizesClientConstructionFailure(t *testing.T) {
	var buildCount int
	orig := newPoliciesClient
	newPoliciesClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		buildCount++
		return nil, errors.New("no credentials configured")
	}
	t.Cleanup(func() { newPoliciesClient = orig })

	resolver := &policyTypeResolver{baseURL: "http://unused.invalid"}
	cmd := newResolverTestCmd(t)

	val1, err1 := resolver.resolve(cmd, "zz-any-id")
	if err1 == nil {
		t.Fatal("expected the first resolve() to surface the client-construction failure")
	}
	if val1 != "" {
		t.Errorf("value on a client-construction failure = %q, want empty", val1)
	}

	val2, err2 := resolver.resolve(cmd, "zz-any-id")
	if err2 != err1 {
		t.Errorf("second resolve() returned a different error: %v (first: %v)", err2, err1)
	}
	if val2 != "" {
		t.Errorf("second resolve() value = %q, want empty", val2)
	}

	if buildCount != 1 {
		t.Errorf("newPoliciesClient was called %d times, want exactly 1 (no re-attempt building the client)", buildCount)
	}
}
