package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
		// wantMsg pins WHICH usage error fired. Cobra's own required-flag error
		// is also exit 2, so a row missing a required flag passes while never
		// reaching the guard it was written for.
		wantMsg string
	}{
		// The registrar guard pins how these flags are REGISTERED; nothing pins
		// how they are READ. Reading one with GetStringArray directly reverts the
		// fix for that flag silently, and only a row here notices.
		{
			name:    "api: --query empty",
			args:    []string{"api", "--path", "/x", "--query", ""},
			wantMsg: "--query requires a non-empty value",
			cmds:    []*cobra.Command{apiCmd},
		},
		{
			name:    "api: --header empty",
			args:    []string{"api", "--path", "/x", "--header", ""},
			wantMsg: "--header requires a non-empty value",
			cmds:    []*cobra.Command{apiCmd},
		},
		{
			name:    "policies search: --policy-type empty",
			args:    []string{"policies", "search", "--policy-type", ""},
			wantMsg: "--policy-type requires a non-empty value",
			cmds:    []*cobra.Command{policiesSearchCmd},
		},
		{
			name:    "policies search: --exclude-policy-id empty",
			args:    []string{"policies", "search", "--exclude-policy-id", ""},
			wantMsg: "--exclude-policy-id requires a non-empty value",
			cmds:    []*cobra.Command{policiesSearchCmd},
		},
		{
			name:    "mcp tools search: --state empty",
			args:    []string{"mcp", "tools", "search", "--app-id", "a", "--connector-id", "c", "--state", ""},
			wantMsg: "--state requires a non-empty value",
			cmds:    []*cobra.Command{mcpToolsSearchCmd},
		},
		{
			name:    "mcp tools search: --classification empty",
			args:    []string{"mcp", "tools", "search", "--app-id", "a", "--connector-id", "c", "--classification", ""},
			wantMsg: "--classification requires a non-empty value",
			cmds:    []*cobra.Command{mcpToolsSearchCmd},
		},
		{
			// markRequired is the only thing stopping {"duration": ""} on the
			// wire; runTaskActionCmd calls RunE directly and never sees cobra's
			// required check, so this row is what pins it.
			name:    "tasks update-grant-duration: --duration missing",
			args:    []string{"tasks", "update-grant-duration", "zz-task-1"},
			wantMsg: `required flag(s) "duration" not set`,
			cmds:    []*cobra.Command{tasksUpdateGrantDurationCmd},
		},
		{
			// The only repeatable flag whose READ was unpinned end to end: the
			// existing --config-field row passes a non-empty bad pair, which
			// parseKeyValues rejects on its own.
			name:    "mcp servers register: --config-field empty",
			args:    []string{"mcp", "servers", "register", "--app-id", "a", "--type", "hosted", "--display-name", "d", "--catalog-id", "cat1", "--config-field", ""},
			wantMsg: "--config-field requires a non-empty value",
			cmds:    []*cobra.Command{mcpServersRegisterCmd},
		},
		{
			name:    "mcp servers register: --user-id empty",
			args:    []string{"mcp", "servers", "register", "--app-id", "a", "--type", "hosted", "--display-name", "d", "--catalog-id", "cat1", "--user-id", ""},
			wantMsg: "--user-id requires a non-empty value",
			cmds:    []*cobra.Command{mcpServersRegisterCmd},
		},
		{
			// --tool-id is a cobra-required flag; omitting it entirely is
			// intercepted by cobra itself (already exitUsage via
			// isCobraUsageError) before RunE ever runs. Passing it as an
			// explicit empty string satisfies "required" (Changed=true) and
			// actually reaches the len(toolIDs)==0 guard this test targets.
			name:    "mcp bindings create: --tool-id empty",
			args:    []string{"mcp", "bindings", "create", "--app-id", "a", "--connector-id", "c", "--toolset-id", "t", "--tool-id", ""},
			wantMsg: "--tool-id requires a non-empty value",
			cmds:    []*cobra.Command{mcpBindingsCreateCmd},
		},
		{
			name:    "mcp bindings delete: --tool-id empty",
			args:    []string{"mcp", "bindings", "delete", "--app-id", "a", "--connector-id", "c", "--toolset-id", "t", "--tool-id", ""},
			wantMsg: "--tool-id requires a non-empty value",
			cmds:    []*cobra.Command{mcpBindingsDeleteCmd},
		},
		{
			// Also pins the ordering fix: mcp_bindings_by_tools.go used to
			// construct its client before this check, unlike create/delete
			// above, so this case would previously have needed real
			// credentials to reach the guard at all.
			name:    "mcp bindings by-tools: --tool-id empty",
			args:    []string{"mcp", "bindings", "by-tools", "--app-id", "a", "--connector-id", "c", "--tool-id", ""},
			wantMsg: "--tool-id requires a non-empty value",
			cmds:    []*cobra.Command{mcpBindingsByToolsCmd},
		},
		{
			name:    "mcp bindings history: neither --toolset-id nor --tool-id",
			args:    []string{"mcp", "bindings", "history", "--app-id", "a", "--connector-id", "c"},
			wantMsg: "exactly one of --toolset-id or --tool-id is required",
			cmds:    []*cobra.Command{mcpBindingsHistoryCmd},
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
			name:    "mcp servers test-connection: invalid --auth",
			args:    []string{"mcp", "servers", "test-connection", "--auth", "bogus"},
			wantMsg: "unsupported --auth",
			cmds:    []*cobra.Command{mcpServersTestConnectionCmd},
		},
		{
			name: "mcp servers test-connection: --server-url and --external-config-file mutually exclusive",
			args: []string{"mcp", "servers", "test-connection", "--server-url", "https://x.example", "--external-config-file", "/nonexistent"},
			cmds: []*cobra.Command{mcpServersTestConnectionCmd},
		},
		{
			// Without wantMsg this passed even with the invalid-type guard
			// deleted: --type bogus falls through the switch and trips a later
			// check that names a flag the user never passed.
			name:    "mcp servers update-credentials: invalid --type",
			args:    []string{"mcp", "servers", "update-credentials", "conn-1", "--app-id", "a", "--type", "bogus"},
			wantMsg: "invalid --type",
			cmds:    []*cobra.Command{mcpServersUpdateCredentialsCmd},
		},
		{
			name: "mcp servers update-credentials: nothing to update",
			args: []string{"mcp", "servers", "update-credentials", "conn-1", "--app-id", "a", "--type", "hosted"},
			cmds: []*cobra.Command{mcpServersUpdateCredentialsCmd},
		},
		{
			name:    "mcp servers update-credentials: invalid --config-field pair",
			args:    []string{"mcp", "servers", "update-credentials", "conn-1", "--app-id", "a", "--type", "hosted", "--config-field", "badpair"},
			wantMsg: "expected key=value",
			cmds:    []*cobra.Command{mcpServersUpdateCredentialsCmd},
		},
		{
			name: "mcp servers update-credentials: --hosted-config-file mutually exclusive with --catalog-id",
			args: []string{"mcp", "servers", "update-credentials", "conn-1", "--app-id", "a", "--type", "hosted", "--catalog-id", "cat-1", "--hosted-config-file", "/nonexistent"},
			cmds: []*cobra.Command{mcpServersUpdateCredentialsCmd},
		},
		{
			// --to-user-id is cobra-required, so omitting it is cobra's job.
			// A lone "" satisfies "required" but pflag collapses it to an
			// empty slice, so only the Changed check sees it.
			name:    "tasks reassign: --to-user-id empty",
			args:    []string{"tasks", "reassign", "task-1", "--to-user-id", ""},
			wantMsg: "--to-user-id requires a non-empty value",
			cmds:    []*cobra.Command{tasksReassignCmd},
		},
		{
			// The shape that shipped broken: under StringSlice the empty
			// occurrence was discarded during parsing and the command posted
			// the surviving id as if that were what was asked for.
			name: "tasks reassign: --to-user-id empty alongside a real one",
			args: []string{"tasks", "reassign", "task-1", "--to-user-id", "", "--to-user-id", "user-b"},
			cmds: []*cobra.Command{tasksReassignCmd},
		},
		{
			name: "apps set-owners: --user-id empty alongside a real one",
			args: []string{"apps", "set-owners", "app-1", "--user-id", "", "--user-id", "user-b"},
			cmds: []*cobra.Command{appsSetOwnersCmd},
		},
		{
			name: "mcp bindings create: --tool-id empty alongside a real one",
			args: []string{"mcp", "bindings", "create", "--app-id", "a", "--connector-id", "c", "--toolset-id", "t", "--tool-id", "", "--tool-id", "tool-b"},
			cmds: []*cobra.Command{mcpBindingsCreateCmd},
		},
		{
			name: "mcp bindings delete: --tool-id empty alongside a real one",
			args: []string{"mcp", "bindings", "delete", "--app-id", "a", "--connector-id", "c", "--toolset-id", "t", "--tool-id", "", "--tool-id", "tool-b"},
			cmds: []*cobra.Command{mcpBindingsDeleteCmd},
		},
		{
			name: "mcp bindings by-tools: --tool-id whitespace alongside a real one",
			args: []string{"mcp", "bindings", "by-tools", "--app-id", "a", "--connector-id", "c", "--tool-id", "   ", "--tool-id", "tool-b"},
			cmds: []*cobra.Command{mcpBindingsByToolsCmd},
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
			if tc.wantMsg != "" && !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error was %q, want it to contain %q — this row is exiting 2 "+
					"for a different reason than the guard it targets", err, tc.wantMsg)
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
