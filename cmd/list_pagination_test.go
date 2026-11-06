package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// listPaginationCase describes one auto-paginating command. Adding a new one
// to this table is one entry, not a bespoke test file; cmd/policies_list.go
// and cmd/policies_search.go also keep their pre-existing bespoke tests
// (cmd/policies_pagination_test.go) as well.
type listPaginationCase struct {
	name   string
	cmd    *cobra.Command
	method string // "GET" or "POST"
	// wantPath is the exact request path this case expects, with any
	// parent-scope ids already substituted via extraFlags below.
	wantPath string
	// args are the command's positional arguments (e.g. a function id).
	args []string
	// extraFlags sets flags this command needs beyond the standard
	// page-size/page-token/limit trio, e.g. {"app-id": "app1"}.
	extraFlags map[string]string
	// page returns the raw JSON response body for a page holding the given
	// item ids (nil/empty = an empty page), ending with nextToken
	// ("" = last page). Every command wraps its rows in a different
	// envelope/shape, so this is the one part each case supplies itself.
	page func(ids []string, nextToken string) string
	// rowIDs extracts, in emission order, the identifying value each item()
	// call varied per row from the command's decoded NDJSON output.
	rowIDs func(rows []map[string]any) []string
	// noRowID marks rows with no id of their own, so the cursor guard can
	// only catch a repeated token, not fresh tokens over the same rows.
	noRowID bool
}

// Every case's command is a package-level singleton shared by the whole test
// binary, so its flags must be reset to defaults between cases.
func resetCmdFlags(t *testing.T, cmd *cobra.Command) {
	t.Helper()
	reset := func() {
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			if sv, ok := f.Value.(pflag.SliceValue); ok {
				_ = sv.Replace(nil)
			} else {
				_ = f.Value.Set(f.DefValue)
			}
			f.Changed = false
		})
	}
	reset()
	t.Cleanup(reset)
}

func decodeJSONBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("reading request body: %v", err)
	}
	var body map[string]any
	if len(b) == 0 {
		return map[string]any{}
	}
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("unmarshaling request body: %v (body: %s)", err, b)
	}
	return body
}

func extractPageToken(t *testing.T, method string, r *http.Request) string {
	t.Helper()
	if method == http.MethodGet {
		return r.URL.Query().Get("page_token")
	}
	body := decodeJSONBody(t, r)
	tok, _ := body["pageToken"].(string)
	return tok
}

func runListPaginationCase(t *testing.T, tc listPaginationCase, srv *httptest.Server) (stdout, stderr string, err error) {
	t.Helper()
	var out bytes.Buffer
	errOut, runErr := runListPaginationCaseTo(t, tc, srv, &out)
	return out.String(), errOut, runErr
}

// runListPaginationCaseTo is runListPaginationCase with a caller-supplied stdout.
func runListPaginationCaseTo(t *testing.T, tc listPaginationCase, srv *httptest.Server, out io.Writer) (stderr string, err error) {
	t.Helper()
	orig, origPolicies := newListClient, newPoliciesClient
	stub := func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	newListClient, newPoliciesClient = stub, stub
	t.Cleanup(func() { newListClient, newPoliciesClient = orig, origPolicies })
	t.Setenv("C1I_URL", "https://example.invalid")

	resetCmdFlags(t, tc.cmd)
	for name, val := range tc.extraFlags {
		if setErr := tc.cmd.Flags().Set(name, val); setErr != nil {
			t.Fatalf("setting --%s=%s: %v", name, val, setErr)
		}
	}

	var errOut bytes.Buffer
	tc.cmd.SetOut(out)
	tc.cmd.SetErr(&errOut)
	tc.cmd.SetContext(context.Background())
	runErr := tc.cmd.RunE(tc.cmd, tc.args)
	return errOut.String(), runErr
}

// jstr renders a Go string as a JSON string literal for hand-built fixture
// bodies below (avoids escaping surprises if an id ever needs one).
func jstr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func listPaginationCases() []listPaginationCase {
	idRows := func(field string) func([]map[string]any) []string {
		return func(rows []map[string]any) []string {
			var ids []string
			for _, r := range rows {
				v, _ := r[field].(string)
				ids = append(ids, v)
			}
			return ids
		}
	}

	// flatPage builds {"<field>":[{"<itemKey>":"<id>"}...],"nextPageToken":"<next>"}.
	flatPage := func(field, itemKey string) func([]string, string) string {
		return func(ids []string, next string) string {
			items := make([]string, 0, len(ids))
			for _, id := range ids {
				items = append(items, fmt.Sprintf(`{%s:%s}`, jstr(itemKey), jstr(id)))
			}
			return fmt.Sprintf(`{%s:[%s],"nextPageToken":%s}`, jstr(field), strings.Join(items, ","), jstr(next))
		}
	}
	// nestedPage builds {"<field>":[{"<wrapKey>":{"<itemKey>":"<id>"}}...],"nextPageToken":"<next>"}.
	nestedPage := func(field, wrapKey, itemKey string) func([]string, string) string {
		return func(ids []string, next string) string {
			items := make([]string, 0, len(ids))
			for _, id := range ids {
				items = append(items, fmt.Sprintf(`{%s:{%s:%s}}`, jstr(wrapKey), jstr(itemKey), jstr(id)))
			}
			return fmt.Sprintf(`{%s:[%s],"nextPageToken":%s}`, jstr(field), strings.Join(items, ","), jstr(next))
		}
	}

	// entitlementPage scopes each entitlement to one app, since the guard keys
	// entitlements on app and id.
	entitlementPage := func(ids []string, next string) string {
		items := make([]string, 0, len(ids))
		for _, id := range ids {
			items = append(items, fmt.Sprintf(`{"appEntitlement":{"id":%s,"appId":"app1"}}`, jstr(id)))
		}
		return fmt.Sprintf(`{"list":[%s],"nextPageToken":%s}`, strings.Join(items, ","), jstr(next))
	}

	return []listPaginationCase{
		{
			name:       "accounts list",
			cmd:        accountsListCmd,
			method:     http.MethodPost,
			wantPath:   "/api/v1/search/app_users",
			extraFlags: map[string]string{"app-id": "app1"},
			page:       nestedPage("list", "appUser", "id"),
			rowIDs:     idRows("id"),
		},
		{
			name:     "roles list",
			cmd:      rolesListCmd,
			method:   http.MethodGet,
			wantPath: "/api/v1/iam/roles",
			page:     flatPage("list", "id"),
			rowIDs:   idRows("id"),
		},
		{
			name:     "apps list",
			cmd:      appsListCmd,
			method:   http.MethodGet,
			wantPath: "/api/v1/apps",
			page:     flatPage("list", "id"),
			rowIDs:   idRows("id"),
		},
		{
			name:     "apps owners",
			cmd:      appsOwnersCmd,
			method:   http.MethodGet,
			wantPath: "/api/v1/apps/app1/owners",
			args:     []string{"app1"},
			page:     flatPage("list", "id"),
			rowIDs:   idRows("id"),
		},
		{
			name:     "automations executions list",
			cmd:      automationsExecutionsListCmd,
			method:   http.MethodPost,
			wantPath: "/api/v1/search/automation_executions",
			page:     nestedPage("list", "automationExecution", "id"),
			rowIDs:   idRows("id"),
		},
		{
			name:     "automations list",
			cmd:      automationsListCmd,
			method:   http.MethodPost,
			wantPath: "/api/v1/search/automations",
			page:     flatPage("list", "id"),
			rowIDs:   idRows("id"),
		},
		{
			name:     "access-profiles list",
			cmd:      accessProfilesListCmd,
			method:   http.MethodGet,
			wantPath: "/api/v1/catalogs",
			page:     nestedPage("list", "requestCatalog", "id"),
			rowIDs:   idRows("id"),
		},
		{
			name:       "connectors list",
			cmd:        connectorsListCmd,
			method:     http.MethodGet,
			wantPath:   "/api/v1/apps/app1/connectors",
			extraFlags: map[string]string{"app-id": "app1"},
			page:       nestedPage("list", "connector", "id"),
			rowIDs:     idRows("id"),
		},
		{
			name:     "entitlements list",
			cmd:      entitlementsListCmd,
			method:   http.MethodPost,
			wantPath: "/api/v1/search/entitlements",
			page:     entitlementPage,
			rowIDs:   idRows("id"),
		},
		{
			name:     "export events",
			noRowID:  true,
			cmd:      exportEventsCmd,
			method:   http.MethodPost,
			wantPath: "/api/v1/systemlog/events",
			page:     flatPage("list", "id"),
			rowIDs:   idRows("id"),
		},
		{
			name:     "functions commits",
			cmd:      functionsCommitsCmd,
			method:   http.MethodGet,
			wantPath: "/api/v1/functions/func1/commits",
			args:     []string{"func1"},
			page:     flatPage("list", "id"),
			rowIDs:   idRows("id"),
		},
		{
			name:     "functions list",
			cmd:      functionsListCmd,
			method:   http.MethodGet,
			wantPath: "/api/v1/functions",
			page:     flatPage("list", "id"),
			rowIDs:   idRows("id"),
		},
		{
			name:     "functions usage",
			cmd:      functionsUsageCmd,
			method:   http.MethodPost,
			wantPath: "/api/v1/search/automations",
			args:     []string{"func1"},
			// functions_usage filters automations client-side by
			// callFunction.functionId, so each row must embed a matching step.
			page: func(ids []string, next string) string {
				items := make([]string, 0, len(ids))
				for _, id := range ids {
					items = append(items, fmt.Sprintf(
						`{"id":%s,"automationSteps":[{"stepName":"s1","callFunction":{"functionId":"func1"}}]}`,
						jstr(id)))
				}
				return fmt.Sprintf(`{"list":[%s],"nextPageToken":%s}`, strings.Join(items, ","), jstr(next))
			},
			rowIDs: idRows("automation_id"),
		},
		{
			name:       "grants list",
			cmd:        grantsListCmd,
			method:     http.MethodPost,
			wantPath:   "/api/v1/search/grants",
			extraFlags: map[string]string{"app-id": "app1"},
			page: func(ids []string, next string) string {
				items := make([]string, 0, len(ids))
				for _, id := range ids {
					items = append(items, fmt.Sprintf(
						`{"appEntitlementUserBinding":{"appUser":{"appUser":{"id":%s}}},"entitlement":{"appEntitlement":{"id":"ent1","appId":"app1"}}}`,
						jstr(id)))
				}
				return fmt.Sprintf(`{"list":[%s],"nextPageToken":%s}`, strings.Join(items, ","), jstr(next))
			},
			rowIDs: idRows("app_user_id"),
		},
		{
			name:     "mcp bindings history",
			noRowID:  true, // entries are {items, metadata}; the fixture's id is ignored
			cmd:      mcpBindingsHistoryCmd,
			method:   http.MethodGet,
			wantPath: "/api/v1/apps/app1/connectors/conn1/mcp_toolsets/ts1/tool_bindings/history",
			extraFlags: map[string]string{
				"app-id": "app1", "connector-id": "conn1", "toolset-id": "ts1",
			},
			page:   flatPage("list", "id"),
			rowIDs: idRows("id"),
		},
		{
			name:     "mcp bindings list",
			noRowID:  true,
			cmd:      mcpBindingsListCmd,
			method:   http.MethodGet,
			wantPath: "/api/v1/apps/app1/connectors/conn1/mcp_toolsets/ts1/tool_bindings",
			extraFlags: map[string]string{
				"app-id": "app1", "connector-id": "conn1", "toolset-id": "ts1",
			},
			page:   flatPage("bindings", "mcpToolId"),
			rowIDs: idRows("mcp_tool_id"),
		},
		{
			name:     "mcp servers catalog list",
			cmd:      mcpServersCatalogListCmd,
			method:   http.MethodGet,
			wantPath: "/api/v1/mcp_server_catalog",
			page:     flatPage("list", "id"),
			rowIDs:   idRows("id"),
		},
		{
			name:     "mcp servers connections list",
			noRowID:  true,
			cmd:      mcpServersConnectionsListCmd,
			method:   http.MethodGet,
			wantPath: "/api/v1/mcp_server_connections",
			page:     flatPage("list", "connectorId"),
			rowIDs:   idRows("connector_id"),
		},
		{
			name:       "mcp servers list",
			cmd:        mcpServersListCmd,
			method:     http.MethodGet,
			wantPath:   "/api/v1/apps/app1/mcp_servers",
			extraFlags: map[string]string{"app-id": "app1"},
			page:       flatPage("list", "connectorId"),
			rowIDs:     idRows("connector_id"),
		},
		{
			name:       "mcp servers search",
			cmd:        mcpServersSearchCmd,
			method:     http.MethodPost,
			wantPath:   "/api/v1/apps/app1/mcp_servers/search",
			extraFlags: map[string]string{"app-id": "app1"},
			page:       nestedPage("list", "mcpServer", "connectorId"),
			rowIDs:     idRows("connector_id"),
		},
		{
			name:     "mcp tools history",
			noRowID:  true, // entries are {metadata, snapshot}; the fixture's id is ignored
			cmd:      mcpToolsHistoryCmd,
			method:   http.MethodGet,
			wantPath: "/api/v1/apps/app1/connectors/conn1/mcp_tools/tool1/history",
			args:     []string{"tool1"},
			extraFlags: map[string]string{
				"app-id": "app1", "connector-id": "conn1",
			},
			page:   flatPage("list", "id"),
			rowIDs: idRows("id"),
		},
		{
			name:     "mcp tools list",
			cmd:      mcpToolsListCmd,
			method:   http.MethodGet,
			wantPath: "/api/v1/apps/app1/connectors/conn1/mcp_tools",
			extraFlags: map[string]string{
				"app-id": "app1", "connector-id": "conn1",
			},
			page:   flatPage("tools", "id"),
			rowIDs: idRows("id"),
		},
		{
			name:     "mcp tools search",
			cmd:      mcpToolsSearchCmd,
			method:   http.MethodPost,
			wantPath: "/api/v1/apps/app1/connectors/conn1/mcp_tools/search",
			extraFlags: map[string]string{
				"app-id": "app1", "connector-id": "conn1",
			},
			page:   flatPage("tools", "id"),
			rowIDs: idRows("id"),
		},
		{
			name:     "mcp toolsets list",
			cmd:      mcpToolsetsListCmd,
			method:   http.MethodGet,
			wantPath: "/api/v1/apps/app1/connectors/conn1/mcp_toolsets",
			extraFlags: map[string]string{
				"app-id": "app1", "connector-id": "conn1",
			},
			page:   flatPage("profiles", "id"),
			rowIDs: idRows("id"),
		},
		{
			name:     "access-profiles requestable-entitlements list",
			cmd:      accessProfilesRequestableEntitlementsListCmd,
			method:   http.MethodGet,
			wantPath: "/api/v1/catalogs/ap1/requestable_entitlements",
			args:     []string{"ap1"},
			page:     entitlementPage,
			rowIDs:   idRows("id"),
		},
		{
			name:     "access-profiles visibility-entitlements list",
			cmd:      accessProfilesVisibilityEntitlementsListCmd,
			method:   http.MethodGet,
			wantPath: "/api/v1/catalogs/ap1/visibility_entitlements",
			args:     []string{"ap1"},
			page:     entitlementPage,
			rowIDs:   idRows("id"),
		},
		{
			name:     "access-reviews list",
			cmd:      accessReviewsListCmd,
			method:   http.MethodGet,
			wantPath: "/api/v1/access_reviews",
			page:     nestedPage("list", "accessReview", "id"),
			rowIDs:   idRows("id"),
		},
		{
			name:     "access-reviews reports list",
			cmd:      accessReviewReportsListCmd,
			method:   http.MethodGet,
			wantPath: "/api/v1/access_review/camp1/report",
			args:     []string{"camp1"},
			page:     flatPage("list", "id"),
			rowIDs:   idRows("id"),
		},
		{
			name:     "findings search",
			cmd:      findingsSearchCmd,
			method:   http.MethodPost,
			wantPath: "/api/v1/findings/search",
			page:     flatPage("list", "id"),
			rowIDs:   idRows("id"),
		},
		{
			name:     "findings audits search",
			cmd:      findingsAuditsSearchCmd,
			method:   http.MethodPost,
			wantPath: "/api/v1/search/finding_audits",
			page: func(ids []string, next string) string {
				items := make([]string, 0, len(ids))
				for _, id := range ids {
					items = append(items, fmt.Sprintf(`{"findingId":"f1","eventId":%s}`, jstr(id)))
				}
				return fmt.Sprintf(`{"list":[%s],"nextPageToken":%s}`, strings.Join(items, ","), jstr(next))
			},
			rowIDs: idRows("event_id"),
		},
		{
			name:     "mcp classifiers list",
			cmd:      mcpClassifiersListCmd,
			method:   http.MethodGet,
			wantPath: "/api/v1/classifiers",
			page:     flatPage("classifiers", "id"),
			rowIDs:   idRows("id"),
		},
		{
			name:     "mcp classifiers bindings list",
			cmd:      mcpClassifierBindingsListCmd,
			method:   http.MethodGet,
			wantPath: "/api/v1/classifier_bindings",
			page:     flatPage("bindings", "id"),
			rowIDs:   idRows("id"),
		},
		{
			name:     "mcp classifiers templates list",
			cmd:      mcpClassifierTemplatesListCmd,
			method:   http.MethodPost,
			wantPath: "/api/v1/classifier_templates/list",
			page:     flatPage("templates", "id"),
			rowIDs:   idRows("id"),
			noRowID:  true,
		},
		{
			name:     "mcp classifiers tool-gates list",
			cmd:      mcpClassifierToolGatesListCmd,
			method:   http.MethodGet,
			wantPath: "/api/v1/tool_gates",
			page:     flatPage("list", "id"),
			rowIDs:   idRows("id"),
		},
		{
			name:     "mcp classifiers tool-gates search",
			cmd:      mcpClassifierToolGatesSearchCmd,
			method:   http.MethodPost,
			wantPath: "/api/v1/search/tool_gates",
			page:     flatPage("list", "id"),
			rowIDs:   idRows("id"),
		},
		{
			name:     "policies list",
			cmd:      policiesListCmd,
			method:   http.MethodGet,
			wantPath: "/api/v1/policies",
			page:     flatPage("list", "id"),
			rowIDs:   idRows("id"),
		},
		{
			name:     "policies search",
			cmd:      policiesSearchCmd,
			method:   http.MethodPost,
			wantPath: "/api/v1/search/policies",
			page:     flatPage("list", "id"),
			rowIDs:   idRows("id"),
		},
		{
			name:     "service-principals list",
			cmd:      servicePrincipalsListCmd,
			method:   http.MethodGet,
			wantPath: "/api/v1/service_principals",
			page:     flatPage("list", "id"),
			rowIDs:   idRows("id"),
		},
		{
			name:     "service-principals credentials list",
			cmd:      spCredentialsListCmd,
			method:   http.MethodGet,
			wantPath: "/api/v1/service_principals/sp1/credentials",
			args:     []string{"sp1"},
			page:     flatPage("list", "id"),
			rowIDs:   idRows("id"),
		},
		{
			name:       "service-principals bindings list",
			cmd:        spBindingsListCmd,
			method:     http.MethodPost,
			wantPath:   "/api/v1/service_principals/bindings/list",
			extraFlags: map[string]string{"function-id": "fn1"},
			page:       flatPage("bindings", "servicePrincipalId"),
			rowIDs:     idRows("service_principal_id"),
			noRowID:    true,
		},
		{
			name:     "requests list",
			cmd:      requestsListCmd,
			method:   http.MethodPost,
			wantPath: "/api/v1/search/tasks",
			// --all skips the /api/v1/auth/introspect lookup requests_list
			// otherwise does to resolve the default "me" scope, which this
			// case's server stub doesn't implement.
			extraFlags: map[string]string{"all": "true"},
			page:       nestedPage("list", "task", "id"),
			rowIDs:     idRows("id"),
		},
		{
			name:     "tasks list",
			cmd:      tasksListCmd,
			method:   http.MethodPost,
			wantPath: "/api/v1/search/tasks",
			page:     nestedPage("list", "task", "id"),
			rowIDs:   idRows("id"),
		},
		{
			name:     "users list",
			cmd:      usersListCmd,
			method:   http.MethodPost,
			wantPath: "/api/v1/search/users",
			page:     nestedPage("list", "user", "id"),
			rowIDs:   idRows("id"),
		},
	}
}

func TestListCommandsPaginateAcrossTwoPages(t *testing.T) {
	for _, tc := range listPaginationCases() {
		t.Run(tc.name, func(t *testing.T) {
			var gotTokens []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.method || r.URL.Path != tc.wantPath {
					t.Errorf("unexpected request: %s %s (want %s %s)", r.Method, r.URL.Path, tc.method, tc.wantPath)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				tok := extractPageToken(t, tc.method, r)
				gotTokens = append(gotTokens, tok)
				w.Header().Set("Content-Type", "application/json")
				switch tok {
				case "":
					_, _ = fmt.Fprint(w, tc.page([]string{"page1-item"}, "tok1"))
				case "tok1":
					_, _ = fmt.Fprint(w, tc.page([]string{"page2-item"}, ""))
				default:
					t.Errorf("unexpected page token: %q", tok)
					w.WriteHeader(http.StatusInternalServerError)
				}
			}))
			defer srv.Close()

			out, _, err := runListPaginationCase(t, tc, srv)
			if err != nil {
				t.Fatalf("RunE: %v", err)
			}

			rows := decodeNDJSONRows(t, out)
			ids := tc.rowIDs(rows)
			want := []string{"page1-item", "page2-item"}
			if len(ids) != len(want) || ids[0] != want[0] || ids[1] != want[1] {
				t.Fatalf("got ids %v, want %v (a break after page 1 would silently emit only page1-item)", ids, want)
			}
			if len(gotTokens) != 2 {
				t.Fatalf("server received %d requests, want 2", len(gotTokens))
			}
			if gotTokens[1] != "tok1" {
				t.Errorf("second request did not carry the token from page 1: got %q, want %q", gotTokens[1], "tok1")
			}
		})
	}
}

// An empty page can still carry a live nextPageToken; the loop must keep
// going on the token, not on whether the page had rows.
func TestListCommandsEmptyPageWithTokenDoesNotStopEarly(t *testing.T) {
	for _, tc := range listPaginationCases() {
		t.Run(tc.name, func(t *testing.T) {
			var requestCount int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount++
				tok := extractPageToken(t, tc.method, r)
				w.Header().Set("Content-Type", "application/json")
				switch tok {
				case "":
					_, _ = fmt.Fprint(w, tc.page(nil, "tok1"))
				case "tok1":
					_, _ = fmt.Fprint(w, tc.page([]string{"page2-item"}, ""))
				default:
					t.Errorf("unexpected page token: %q", tok)
					w.WriteHeader(http.StatusInternalServerError)
				}
			}))
			defer srv.Close()

			out, _, err := runListPaginationCase(t, tc, srv)
			if err != nil {
				t.Fatalf("RunE: %v", err)
			}

			if requestCount != 2 {
				t.Fatalf("server received %d requests, want 2 (an empty page must not stop pagination)", requestCount)
			}
			rows := decodeNDJSONRows(t, out)
			ids := tc.rowIDs(rows)
			if len(ids) != 1 || ids[0] != "page2-item" {
				t.Errorf("expected exactly the one row from page 2, got: %v", ids)
			}
		})
	}
}

// Asserts the request COUNT, not just output: output alone can't tell one
// request from two if the server hands back a full page either way.
func TestListCommandsExplicitPageTokenDisablesAutoPagination(t *testing.T) {
	for _, tc := range listPaginationCases() {
		t.Run(tc.name, func(t *testing.T) {
			var requestCount int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount++
				tok := extractPageToken(t, tc.method, r)
				if tok != "manual-tok" {
					t.Errorf("page token = %q, want %q", tok, "manual-tok")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, tc.page([]string{"only-item"}, "more-available"))
			}))
			defer srv.Close()

			if tc.extraFlags == nil {
				tc.extraFlags = map[string]string{}
			}
			tc.extraFlags["page-token"] = "manual-tok"
			out, _, err := runListPaginationCase(t, tc, srv)
			if err != nil {
				t.Fatalf("RunE: %v", err)
			}

			if requestCount != 1 {
				t.Fatalf("server received %d requests, want exactly 1 (--page-token should disable auto-pagination)", requestCount)
			}
			rows := decodeNDJSONRows(t, out)
			ids := tc.rowIDs(rows)
			if len(ids) != 1 || ids[0] != "only-item" {
				t.Errorf("expected exactly the one page's row, got: %v", ids)
			}
		})
	}
}

// Every list command, against a server whose cursor stops advancing: stop,
// warn naming the route, exit 6. Two shapes: the same token re-issued, and
// fresh tokens over the same rows (detectable only when rows carry an id).
// The request cap fails an unguarded loop promptly instead of hanging.
func TestListCommandsStuckCursorReturnsExitServer(t *testing.T) {
	// Enough rows that two stale pages meet stuckStaleRows.
	var stuckIDs []string
	for i := 0; i < stuckStaleRows; i++ {
		stuckIDs = append(stuckIDs, fmt.Sprintf("stuck-item-%d", i))
	}
	for _, tc := range listPaginationCases() {
		route := tc.method + " " + tc.wantPath
		for _, freshTokens := range []bool{false, true} {
			if freshTokens && tc.noRowID {
				continue
			}
			name := tc.name + "/repeated token"
			if freshTokens {
				name = tc.name + "/fresh tokens"
			}
			t.Run(name, func(t *testing.T) {
				var requestCount int
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requestCount++
					if requestCount > 4 {
						t.Errorf("server received a 5th request; auto-pagination did not terminate")
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					tok := extractPageToken(t, tc.method, r)
					next := "tok-same"
					if freshTokens {
						next = fmt.Sprintf("tok-v%d", requestCount)
					}
					w.Header().Set("Content-Type", "application/json")
					switch tok {
					case "", "tok-same", "tok-v1", "tok-v2":
						_, _ = fmt.Fprint(w, tc.page(stuckIDs, next))
					default:
						t.Errorf("unexpected page token: %q", tok)
						w.WriteHeader(http.StatusInternalServerError)
					}
				}))
				defer srv.Close()

				out, errOut, err := runListPaginationCase(t, tc, srv)
				if err == nil {
					t.Fatalf("RunE: want an error (stuck cursor -> exit 6), got nil; stdout=%q stderr=%q", out, errOut)
				}
				if got, want := exitCode(err), exitServer; got != want {
					t.Errorf("exitCode(err) = %d, want %d (exitServer, \"C1 failed\"); err=%v", got, want, err)
				}
				// A repeated token is caught before re-sending it; fresh tokens
				// need the page to repeat twice before the cursor counts as stuck.
				wantRequests, wantRows := 2, len(stuckIDs)
				if freshTokens {
					wantRequests = 3
				} else if tc.noRowID {
					wantRows = 2 * len(stuckIDs) // no id, so the repeated page can't be recognized
				}
				if requestCount != wantRequests {
					t.Fatalf("server received %d requests, want exactly %d", requestCount, wantRequests)
				}
				ids := tc.rowIDs(decodeNDJSONRows(t, out))
				if len(ids) != wantRows {
					t.Fatalf("got %d rows, want %d (a stale page must not be re-emitted)", len(ids), wantRows)
				}
				if !strings.Contains(errOut, "INCOMPLETE") || !strings.Contains(errOut, route) {
					t.Errorf("stderr warning = %q, want it to say results are INCOMPLETE and name %s", errOut, route)
				}
			})
		}
	}
}
