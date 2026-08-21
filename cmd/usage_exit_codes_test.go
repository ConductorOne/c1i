package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// resetCmds resets every flag on each given command to its default via
// resetCmdFlags (cmd/list_pagination_test.go), which also schedules its own
// t.Cleanup restore.
func resetCmds(t *testing.T, cmds ...*cobra.Command) {
	t.Helper()
	for _, c := range cmds {
		resetCmdFlags(t, c)
	}
}

// TestValidationGuardsExitUsage is a systemic regression test for the exit-code
// defect fixed across this repo: a bad-flags/args condition returning a bare
// fmt.Errorf classified as the generic exitError (1) instead of the documented
// exitUsage (2). Each case here previously exited 1; every one must now exit 2.
//
// Every case drives the real command tree via rootCmd.ExecuteContext (never a
// stub of the guard itself), so this proves the fix along the same path a user
// hits it. C1I_URL is set to a placeholder so GetBaseURL succeeds; no command
// below reaches a network call before returning its usage error, so no
// credentials or httptest server are needed.
func TestValidationGuardsExitUsage(t *testing.T) {
	// functions_list constructs its client (newListClient) before checking
	// --published-only/--draft-only, so that seam must be stubbed to avoid a
	// real credential/network dependency; the stub is never actually called.
	origNewListClient := newListClient
	newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting("http://192.0.2.1:1", &http.Client{}), nil
	}
	t.Cleanup(func() { newListClient = origNewListClient })

	cases := []struct {
		name string
		args []string
		cmds []*cobra.Command // commands whose flags need resetting between cases
	}{
		{
			// --tool-id is a cobra-required flag; omitting it entirely is
			// intercepted by cobra itself (already exitUsage via
			// isCobraUsageError) before RunE ever runs. Passing it as an
			// explicit empty string satisfies "required" (Changed=true) and
			// actually reaches the len(toolIDs)==0 guard this test targets.
			name: "mcp bindings create: --tool-id empty",
			args: []string{"mcp", "bindings", "create", "--app-id", "a", "--connector-id", "c", "--toolset-id", "t", "--tool-id", ""},
			cmds: []*cobra.Command{mcpBindingsCreateCmd},
		},
		{
			name: "mcp bindings delete: --tool-id empty",
			args: []string{"mcp", "bindings", "delete", "--app-id", "a", "--connector-id", "c", "--toolset-id", "t", "--tool-id", ""},
			cmds: []*cobra.Command{mcpBindingsDeleteCmd},
		},
		{
			// Also pins the ordering fix: mcp_bindings_by_tools.go used to
			// construct its client before this check, unlike create/delete
			// above, so this case would previously have needed real
			// credentials to reach the guard at all.
			name: "mcp bindings by-tools: --tool-id empty",
			args: []string{"mcp", "bindings", "by-tools", "--app-id", "a", "--connector-id", "c", "--tool-id", ""},
			cmds: []*cobra.Command{mcpBindingsByToolsCmd},
		},
		{
			name: "mcp bindings history: neither --toolset-id nor --tool-id",
			args: []string{"mcp", "bindings", "history", "--app-id", "a", "--connector-id", "c"},
			cmds: []*cobra.Command{mcpBindingsHistoryCmd},
		},
		{
			name: "mcp bindings history: --toolset-id and --tool-id both set",
			args: []string{"mcp", "bindings", "history", "--app-id", "a", "--connector-id", "c", "--toolset-id", "t", "--tool-id", "x"},
			cmds: []*cobra.Command{mcpBindingsHistoryCmd},
		},
		{
			name: "functions list: --published-only and --draft-only",
			args: []string{"functions", "list", "--published-only", "--draft-only"},
			cmds: []*cobra.Command{functionsListCmd},
		},
		{
			name: "mcp servers update: nothing to update",
			args: []string{"mcp", "servers", "update", "conn-1", "--app-id", "a"},
			cmds: []*cobra.Command{mcpServersUpdateCmd},
		},
		{
			name: "mcp toolsets update: nothing to update",
			args: []string{"mcp", "toolsets", "update", "toolset-1", "--app-id", "a", "--connector-id", "c"},
			cmds: []*cobra.Command{mcpToolsetsUpdateCmd},
		},
		{
			name: "mcp tools approve: --state removed",
			args: []string{"mcp", "tools", "approve", "tool-1", "--app-id", "a", "--connector-id", "c", "--state", "removed"},
			cmds: []*cobra.Command{mcpToolsApproveCmd},
		},
		{
			name: "mcp servers test-connection: edit mode with no <connector-id>",
			args: []string{"mcp", "servers", "test-connection", "--app-id", "a"},
			cmds: []*cobra.Command{mcpServersTestConnectionCmd},
		},
		{
			name: "mcp servers test-connection: no config at all",
			args: []string{"mcp", "servers", "test-connection"},
			cmds: []*cobra.Command{mcpServersTestConnectionCmd},
		},
		{
			name: "mcp servers test-connection: invalid --auth",
			args: []string{"mcp", "servers", "test-connection", "--auth", "bogus"},
			cmds: []*cobra.Command{mcpServersTestConnectionCmd},
		},
		{
			name: "mcp servers test-connection: --url and --external-config-file mutually exclusive",
			args: []string{"mcp", "servers", "test-connection", "--url", "https://x.example", "--external-config-file", "/nonexistent"},
			cmds: []*cobra.Command{mcpServersTestConnectionCmd},
		},
		{
			name: "mcp servers update-credentials: invalid --type",
			args: []string{"mcp", "servers", "update-credentials", "conn-1", "--app-id", "a", "--type", "bogus"},
			cmds: []*cobra.Command{mcpServersUpdateCredentialsCmd},
		},
		{
			name: "mcp servers update-credentials: nothing to update",
			args: []string{"mcp", "servers", "update-credentials", "conn-1", "--app-id", "a", "--type", "hosted"},
			cmds: []*cobra.Command{mcpServersUpdateCredentialsCmd},
		},
		{
			name: "mcp servers update-credentials: invalid --config-field pair",
			args: []string{"mcp", "servers", "update-credentials", "conn-1", "--app-id", "a", "--type", "hosted", "--config-field", "badpair"},
			cmds: []*cobra.Command{mcpServersUpdateCredentialsCmd},
		},
		{
			name: "mcp servers update-credentials: --hosted-config-file mutually exclusive with --catalog-id",
			args: []string{"mcp", "servers", "update-credentials", "conn-1", "--app-id", "a", "--type", "hosted", "--catalog-id", "cat-1", "--hosted-config-file", "/nonexistent"},
			cmds: []*cobra.Command{mcpServersUpdateCredentialsCmd},
		},
		{
			name: "auth login: --client-id without --client-secret",
			args: []string{"auth", "login", "--client-id", "foo"},
			cmds: []*cobra.Command{authLoginCmd},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			resetCmds(t, tc.cmds...)
			t.Setenv("C1I_URL", "https://example.invalid")

			err := runRootWithArgs(t, tc.args)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if got, want := exitCode(err), exitUsage; got != want {
				t.Errorf("exitCode(%v) = %d, want %d (exitUsage); err type %T", err, got, want, err)
			}
		})
	}
}

// TestDocsEndpointUnknownTargetExitsUsage pins docs_openapi.go's "endpoint %s
// not found" (a bad positional argument, like docs_guide.go's "unknown guide")
// as exitUsage rather than the generic exitError it returned before.
func TestDocsEndpointUnknownTargetExitsUsage(t *testing.T) {
	primeOpenAPICache(t)

	err := docsEndpointCmd.RunE(docsEndpointCmd, []string{"/does/not/exist"})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if got, want := exitCode(err), exitUsage; got != want {
		t.Errorf("exitCode(%v) = %d, want %d (exitUsage); err type %T", err, got, want, err)
	}
}

// TestDeriveGatewayURLBadInputExitsUsage pins mcp_gateway.go's
// "cannot derive gateway URL" -- an unparseable/hostless base URL is caller
// input (a bad --url/--gateway-url), so it must classify as exitUsage.
func TestDeriveGatewayURLBadInputExitsUsage(t *testing.T) {
	_, err := deriveGatewayURL("://not a url")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if got, want := exitCode(err), exitUsage; got != want {
		t.Errorf("exitCode(%v) = %d, want %d (exitUsage); err type %T", err, got, want, err)
	}
}

// TestResolveMCPServerCatalogIDMissingExitsUsage pins
// mcp_servers_update_credentials.go's "server has no catalog id; pass
// --catalog-id explicitly" -- the server response gives the caller nothing to
// work with unless they supply the flag themselves, so it's a usage error, not
// the generic exitError a bare fmt.Errorf produced before.
func TestResolveMCPServerCatalogIDMissingExitsUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"mcpServer":{}}`)
	}))
	defer srv.Close()

	c := client.NewForTesting(srv.URL, srv.Client())
	_, err := resolveMCPServerCatalogID(context.Background(), c, "app-1", "conn-1")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if got, want := exitCode(err), exitUsage; got != want {
		t.Errorf("exitCode(%v) = %d, want %d (exitUsage); err type %T", err, got, want, err)
	}
}

// TestResolvePolicyStepIDUnderivableRequiredExitsUsage pins tasks.go's "could
// not determine the current policy step" (approve's required=true path) as
// exitUsage: the task has no current step, so the fix is for the caller to
// pass --policy-step-id, not a retry.
func TestResolvePolicyStepIDUnderivableRequiredExitsUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// taskView.task.policy.current.id is absent/empty.
		_, _ = fmt.Fprint(w, `{"taskView":{"task":{"id":"task-1","policy":{"current":{}}}}}`)
	}))
	defer srv.Close()

	c := client.NewForTesting(srv.URL, srv.Client())
	_, err := resolvePolicyStepID(context.Background(), c, "task-1", "", true)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if got, want := exitCode(err), exitUsage; got != want {
		t.Errorf("exitCode(%v) = %d, want %d (exitUsage); err type %T", err, got, want, err)
	}
}

// --- "could not determine the current user" cases: these need a fake
// /api/v1/auth/introspect that resolves to an empty userId, driven through
// each command's own client-injection seam (mirrors stubNewGrantClient /
// stubNewRevokeClient in requests_create_test.go and newListClient's stub in
// list_pagination_test.go). ---

func emptyUserIDIntrospectServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/introspect":
			_, _ = fmt.Fprint(w, `{"userId":""}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRequestsCreateGrantUnresolvableUserExitsUsage(t *testing.T) {
	srv := emptyUserIDIntrospectServer(t)
	resetGrantCmdFlags(t)
	stubNewGrantClient(t, srv)
	t.Setenv("C1I_URL", "https://example.invalid")

	_ = requestsCreateGrantCmd.Flags().Set("app-id", "app1")
	_ = requestsCreateGrantCmd.Flags().Set("entitlement-id", "ent1")
	requestsCreateGrantCmd.SetContext(context.Background())

	err := requestsCreateGrantCmd.RunE(requestsCreateGrantCmd, nil)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if got, want := exitCode(err), exitUsage; got != want {
		t.Errorf("exitCode(%v) = %d, want %d (exitUsage); err type %T", err, got, want, err)
	}
}

func TestRequestsCreateRevokeUnresolvableUserExitsUsage(t *testing.T) {
	srv := emptyUserIDIntrospectServer(t)
	t.Setenv("C1I_URL", "https://example.invalid")

	orig := newRevokeClient
	newRevokeClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newRevokeClient = orig })

	resetCmdFlags(t, requestsCreateRevokeCmd)
	_ = requestsCreateRevokeCmd.Flags().Set("app-id", "app1")
	_ = requestsCreateRevokeCmd.Flags().Set("entitlement-id", "ent1")
	requestsCreateRevokeCmd.SetContext(context.Background())

	err := requestsCreateRevokeCmd.RunE(requestsCreateRevokeCmd, nil)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if got, want := exitCode(err), exitUsage; got != want {
		t.Errorf("exitCode(%v) = %d, want %d (exitUsage); err type %T", err, got, want, err)
	}
}

func TestRequestsListUnresolvableUserExitsUsage(t *testing.T) {
	srv := emptyUserIDIntrospectServer(t)
	t.Setenv("C1I_URL", "https://example.invalid")

	orig := newListClient
	newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newListClient = orig })

	resetCmdFlags(t, requestsListCmd)
	requestsListCmd.SetContext(context.Background())

	err := requestsListCmd.RunE(requestsListCmd, nil)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if got, want := exitCode(err), exitUsage; got != want {
		t.Errorf("exitCode(%v) = %d, want %d (exitUsage); err type %T", err, got, want, err)
	}
}

func TestTasksListAssignedToMeUnresolvableUserExitsUsage(t *testing.T) {
	srv := emptyUserIDIntrospectServer(t)
	t.Setenv("C1I_URL", "https://example.invalid")

	orig := newListClient
	newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newListClient = orig })

	resetCmdFlags(t, tasksListCmd)
	_ = tasksListCmd.Flags().Set("assigned-to-me", "true")
	tasksListCmd.SetContext(context.Background())

	err := tasksListCmd.RunE(tasksListCmd, nil)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if got, want := exitCode(err), exitUsage; got != want {
		t.Errorf("exitCode(%v) = %d, want %d (exitUsage); err type %T", err, got, want, err)
	}
}
