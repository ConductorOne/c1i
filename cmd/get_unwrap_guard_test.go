package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Every typed `get` must expose the resource's identifier at the top level of
// its output, as list rows do. getUnwrapCases is the table; each body is the
// envelope that endpoint really answers with.

type getUnwrapCase struct {
	name string
	cmd  *cobra.Command
	args []string
	// flags are the parent-scope ids the command requires (--app-id, ...).
	flags map[string]string
	// idKey is the identifying field: "id" except for MCP servers.
	idKey string
	body  string
	// wantKeys is the EXACT top-level key set the output must have: the
	// payload's own keys plus every envelope key beside it. Asserting the whole
	// set, not just that the id survived, is what catches a hoist that also
	// leaks or loses a key.
	wantKeys []string
	// payloadPath is where the resource sits inside body. Declared, not
	// derived, so the value assertions don't re-implement the search they check.
	payloadPath []string
}

func getUnwrapCases() []getUnwrapCase {
	return []getUnwrapCase{
		{
			name:        "apps get",
			cmd:         appsGetCmd,
			args:        []string{"app-1"},
			idKey:       "id",
			body:        `{"app":{"id":"app-1","displayName":"Okta"}}`,
			payloadPath: []string{"app"},
			wantKeys:    []string{"id", "displayName"},
		},
		{
			name:        "policies get",
			cmd:         policiesGetCmd,
			args:        []string{"pol-1"},
			idKey:       "id",
			body:        `{"policy":{"id":"pol-1","displayName":"Nightly review"}}`,
			payloadPath: []string{"policy"},
			wantKeys:    []string{"id", "displayName"},
		},
		{
			name:        "automations get",
			cmd:         automationsGetCmd,
			args:        []string{"aut-1"},
			idKey:       "id",
			body:        `{"automation":{"id":"aut-1","displayName":"Nightly"}}`,
			payloadPath: []string{"automation"},
			wantKeys:    []string{"id", "displayName"},
		},
		{
			name:        "functions get",
			cmd:         functionsGetCmd,
			args:        []string{"fn-1"},
			idKey:       "id",
			body:        `{"function":{"id":"fn-1","displayName":"scorer"}}`,
			payloadPath: []string{"function"},
			wantKeys:    []string{"id", "displayName"},
		},
		{
			name:        "users get",
			cmd:         usersGetCmd,
			args:        []string{"usr-1"},
			idKey:       "id",
			body:        `{"userView":{"user":{"id":"usr-1","email":"a@b.example"},"userId":"usr-1","rolesPath":"","objectPermissions":{"read":true}},"expanded":[{"id":"role-1"}]}`,
			wantKeys:    []string{"id", "email", "userId", "rolesPath", "objectPermissions", "expanded"},
			payloadPath: []string{"userView", "user"},
		},
		{
			name:        "requests get",
			cmd:         requestsGetCmd,
			args:        []string{"req-1"},
			idKey:       "id",
			body:        `{"taskView":{"task":{"id":"req-1","state":"TASK_STATE_OPEN"},"userPath":"","appPath":"","objectPermissions":null},"expanded":[{"id":"ent-1"}]}`,
			wantKeys:    []string{"id", "state", "userPath", "appPath", "objectPermissions", "expanded"},
			payloadPath: []string{"taskView", "task"},
		},
		{
			name:        "entitlements get",
			cmd:         entitlementsGetCmd,
			args:        []string{"ent-1"},
			flags:       map[string]string{"app-id": "app-1"},
			idKey:       "id",
			body:        `{"appEntitlementView":{"appEntitlement":{"id":"ent-1","displayName":"Admin"},"appPath":"","objectPermissions":{"read":true}},"expanded":[{"id":"app-1"}]}`,
			wantKeys:    []string{"id", "displayName", "appPath", "objectPermissions", "expanded"},
			payloadPath: []string{"appEntitlementView", "appEntitlement"},
		},
		{
			name:        "mcp servers get",
			cmd:         mcpServersGetCmd,
			args:        []string{"conn-1"},
			flags:       map[string]string{"app-id": "app-1"},
			idKey:       "connectorId",
			body:        `{"mcpServer":{"connectorId":"conn-1","appId":"app-1","displayName":"Datadog"}}`,
			payloadPath: []string{"mcpServer"},
			wantKeys:    []string{"connectorId", "appId", "displayName"},
		},
		{
			name:        "mcp tools get",
			cmd:         mcpToolsGetCmd,
			args:        []string{"tool-1"},
			flags:       map[string]string{"app-id": "app-1", "connector-id": "conn-1"},
			idKey:       "id",
			body:        `{"tool":{"id":"tool-1","name":"dd_search"}}`,
			payloadPath: []string{"tool"},
			wantKeys:    []string{"id", "name"},
		},
		{
			name:        "mcp toolsets get",
			cmd:         mcpToolsetsGetCmd,
			args:        []string{"ts-1"},
			flags:       map[string]string{"app-id": "app-1", "connector-id": "conn-1"},
			idKey:       "id",
			body:        `{"profile":{"id":"ts-1","displayName":"read-only"}}`,
			payloadPath: []string{"profile"},
			wantKeys:    []string{"id", "displayName"},
		},
		{
			name:        "mcp toolsets get-by-entitlement",
			cmd:         mcpToolsetsGetByEntitlementCmd,
			args:        []string{"ent-1"},
			flags:       map[string]string{"app-id": "app-1"},
			idKey:       "id",
			body:        `{"profile":{"id":"ts-1","appEntitlementId":"ent-1"}}`,
			payloadPath: []string{"profile"},
			wantKeys:    []string{"id", "appEntitlementId"},
		},
		{
			// The catalog sits two levels down and memberCount rides beside
			// it on the view, so a hoist that took only requestCatalogView
			// would strip the id and one that took only requestCatalog would
			// drop the count.
			name:        "access-profiles get",
			cmd:         accessProfilesGetCmd,
			args:        []string{"cat-1"},
			idKey:       "id",
			body:        `{"requestCatalogView":{"requestCatalog":{"id":"cat-1","displayName":"Engineering","published":true},"memberCount":"7","createdByUserPath":"","accessEntitlementsPath":""},"expanded":[{"id":"ent-1"}]}`,
			payloadPath: []string{"requestCatalogView", "requestCatalog"},
			wantKeys:    []string{"id", "displayName", "published", "memberCount", "createdByUserPath", "accessEntitlementsPath", "expanded"},
		},
		{
			name:        "mcp servers catalog get",
			cmd:         mcpServersCatalogGetCmd,
			args:        []string{"cat-1"},
			idKey:       "id",
			body:        `{"catalogEntry":{"id":"cat-1","serviceName":"datadog"}}`,
			payloadPath: []string{"catalogEntry"},
			wantKeys:    []string{"id", "serviceName"},
		},
	}
}

// stubGetClient is stubSearchClient (cmd/mcp_servers_test.go) for the
// single-object read path; policies get builds its client through its own var.
func stubGetClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	stub := func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	orig, origPolicies := newClient, newPoliciesClient
	newClient, newPoliciesClient = stub, stub
	t.Cleanup(func() { newClient, newPoliciesClient = orig, origPolicies })
}

// runGetCase drives one case's RunE against a stub API answering with its
// envelope, and returns the command's stdout.
func runGetCase(t *testing.T, c getUnwrapCase) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, c.body)
	}))
	defer srv.Close()

	resetCmdFlags(t, c.cmd)
	stubGetClient(t, srv)
	t.Setenv("C1I_URL", "https://example.invalid")
	for name, val := range c.flags {
		if err := c.cmd.Flags().Set(name, val); err != nil {
			t.Fatalf("set --%s: %v", name, err)
		}
	}

	var out bytes.Buffer
	c.cmd.SetOut(&out)
	t.Cleanup(func() { c.cmd.SetOut(nil) })
	c.cmd.SetContext(context.Background())
	if err := c.cmd.RunE(c.cmd, c.args); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	return out.String()
}

// TestTypedGetExposesTopLevelID is the guard: `jq -r .id` (or .connectorId for
// an MCP server) must resolve on every typed get's output, and no envelope key
// may be lost on the way.
func TestTypedGetExposesTopLevelID(t *testing.T) {
	for _, c := range getUnwrapCases() {
		t.Run(c.name, func(t *testing.T) {
			var got map[string]any
			raw := runGetCase(t, c)
			if err := json.Unmarshal([]byte(raw), &got); err != nil {
				t.Fatalf("output is not a JSON object: %v\noutput: %s", err, raw)
			}
			id, ok := got[c.idKey]
			if !ok {
				t.Fatalf("output has no top-level %q (keys: %v)\noutput: %s", c.idKey, sortedKeys(got), raw)
			}
			if s, isStr := id.(string); !isStr || s == "" {
				t.Errorf("top-level %q = %v, want a non-empty string", c.idKey, id)
			}
			want := append([]string{}, c.wantKeys...)
			sort.Strings(want)
			if gotKeys := sortedKeys(got); !reflect.DeepEqual(gotKeys, want) {
				t.Errorf("top-level keys = %v, want exactly %v", gotKeys, want)
			}
			// Values, not just names: the payload's own keys and each sibling
			// passed on the way down must arrive carrying what they held.
			for key, val := range flattenEnvelope(t, c) {
				if !reflect.DeepEqual(got[key], val) {
					t.Errorf("%q = %#v, want %#v", key, got[key], val)
				}
			}
		})
	}
}

// TestTypedGetFieldsProjectsUnwrapped pins the composition order: --fields
// applies after unwrapping, so `--fields id` yields {"id":"…"} rather than
// rebuilding the wrapper around it.
func TestTypedGetFieldsProjectsUnwrapped(t *testing.T) {
	for _, c := range getUnwrapCases() {
		t.Run(c.name, func(t *testing.T) {
			setFieldsFlag(t, c.idKey)
			raw := runGetCase(t, c)
			var got map[string]any
			if err := json.Unmarshal([]byte(raw), &got); err != nil {
				t.Fatalf("output is not a JSON object: %v\noutput: %s", err, raw)
			}
			if len(got) != 1 {
				t.Fatalf("--fields %s produced %v, want just that one top-level key", c.idKey, sortedKeys(got))
			}
			if _, ok := got[c.idKey]; !ok {
				t.Errorf("--fields %s produced %v", c.idKey, sortedKeys(got))
			}
		})
	}
}

// TestTypedGetZeroMatchFieldsStillExitsUsage: unwrapping must not soften the
// zero-match rule — a --fields naming nothing in the response is still exit 2,
// not an empty object at exit 0.
func TestTypedGetZeroMatchFieldsStillExitsUsage(t *testing.T) {
	c := getUnwrapCases()[0]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, c.body)
	}))
	defer srv.Close()

	resetCmdFlags(t, c.cmd)
	stubGetClient(t, srv)
	t.Setenv("C1I_URL", "https://example.invalid")
	setFieldsFlag(t, "totally_bogus_field")

	var out bytes.Buffer
	c.cmd.SetOut(&out)
	t.Cleanup(func() { c.cmd.SetOut(nil) })
	c.cmd.SetContext(context.Background())
	err := c.cmd.RunE(c.cmd, c.args)
	if err == nil {
		t.Fatalf("expected a usage error, got nil and output %q", out.String())
	}
	if got, want := exitCode(err), exitUsage; got != want {
		t.Errorf("exitCode = %d, want %d (err: %v)", got, want, err)
	}
}

// TestEveryTypedGetIsCovered fails when a `get`-shaped command anywhere in the
// tree is missing from getUnwrapCases, so a twelfth one can't ship unwrapped
// and untested.
func TestEveryTypedGetIsCovered(t *testing.T) {
	covered := map[string]bool{}
	for _, c := range getUnwrapCases() {
		covered[c.cmd.CommandPath()] = true
	}

	var missing []string
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		for _, sub := range cmd.Commands() {
			name := sub.Name()
			if (name == "get" || strings.HasPrefix(name, "get-")) && !covered[sub.CommandPath()] {
				missing = append(missing, sub.CommandPath())
			}
			walk(sub)
		}
	}
	walk(rootCmd)

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("these get commands are not covered by getUnwrapCases: %v\n"+
			"add them (and route their output through writeResource) so top-level id stays guaranteed", missing)
	}
}

// setFieldsFlag sets the --fields projection through viper, the same override
// the rest of this package's tests use (a viper.Set outranks both the flag and
// C1I_FIELDS, so anything less loses to another test's leftover).
func setFieldsFlag(t *testing.T, value string) {
	t.Helper()
	viper.Set("fields", value)
	t.Cleanup(func() { viper.Set("fields", "") })
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// flattenEnvelope returns what unwrapping c.body must produce: the payload's
// keys plus every key sitting beside a level of c.payloadPath.
func flattenEnvelope(t *testing.T, c getUnwrapCase) map[string]any {
	t.Helper()
	var level map[string]any
	if err := json.Unmarshal([]byte(c.body), &level); err != nil {
		t.Fatalf("fixture body is not JSON: %v", err)
	}
	want := map[string]any{}
	for i, seg := range c.payloadPath {
		for k, v := range level {
			if k != seg {
				want[k] = v
			}
		}
		next, ok := level[seg].(map[string]any)
		if !ok {
			t.Fatalf("fixture payloadPath %v does not resolve at segment %d", c.payloadPath, i)
		}
		level = next
	}
	for k, v := range level {
		want[k] = v
	}
	return want
}
