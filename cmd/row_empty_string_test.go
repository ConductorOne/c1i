package cmd

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/ConductorOne/c1i/internal/mcpgateway"
	"github.com/spf13/cobra"
)

// A row field with nothing to report must be absent or null, never "": an
// empty string is truthy in jq, so `select(.field)` would match every row.
// Each check feeds a minimal row (most fields absent) and fails on a ""
// anywhere in it, nested objects and arrays included.

// emptyStringPaths lists the paths of every "" inside v.
func emptyStringPaths(v any, path string) []string {
	var out []string
	switch x := v.(type) {
	case string:
		if x == "" {
			out = append(out, path)
		}
	case map[string]any:
		for k, e := range x {
			out = append(out, emptyStringPaths(e, path+"."+k)...)
		}
	case []any:
		for i, e := range x {
			out = append(out, emptyStringPaths(e, fmt.Sprintf("%s[%d]", path, i))...)
		}
	}
	sort.Strings(out)
	return out
}

func assertNoEmptyStrings(t *testing.T, what string, rows []map[string]any) {
	t.Helper()
	if len(rows) == 0 {
		t.Fatalf("%s emitted no rows", what)
	}
	for _, row := range rows {
		if paths := emptyStringPaths(row, ""); len(paths) > 0 {
			t.Errorf("%s emits \"\" at %v; omit the key or emit null", what, paths)
		}
	}
}

func TestListRowsNeverContainEmptyString(t *testing.T) {
	for _, tc := range listPaginationCases() {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, tc.page([]string{"x1"}, ""))
			}))
			defer srv.Close()
			out, _, err := runListPaginationCase(t, tc, srv)
			if err != nil {
				t.Fatalf("RunE: %v", err)
			}
			assertNoEmptyStrings(t, tc.name, decodeNDJSONRows(t, out))
		})
	}
}

// Commands outside the pagination table, each with a minimal reply.
func TestOtherListCommandsNeverEmitEmptyString(t *testing.T) {
	cases := []struct {
		name string
		body string
		run  func() error
	}{
		{"mcp toolsets requestable-connectors", `{"connectors":[{"connectorId":"c1"}]}`, func() error {
			return mcpToolsetsRequestableConnectorsCmd.RunE(mcpToolsetsRequestableConnectorsCmd, []string{"u1"})
		}},
		{"mcp bindings by-tools", `{"accessProfilesForTools":[{"accessProfiles":[{"id":"p1"}]}]}`, func() error {
			_ = mcpBindingsByToolsCmd.Flags().Set("app-id", "a")
			_ = mcpBindingsByToolsCmd.Flags().Set("connector-id", "c")
			_ = mcpBindingsByToolsCmd.Flags().Set("tool-id", "t")
			return mcpBindingsByToolsCmd.RunE(mcpBindingsByToolsCmd, nil)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			stubNewClient(t, srv)
			t.Setenv("C1I_URL", "https://example.invalid")
			var out bytes.Buffer
			for _, c := range []*cobra.Command{mcpToolsetsRequestableConnectorsCmd, mcpBindingsByToolsCmd} {
				c.SetOut(&out)
				c.SetContext(context.Background())
			}
			if err := tc.run(); err != nil {
				t.Fatal(err)
			}
			assertNoEmptyStrings(t, tc.name, decodeNDJSONRows(t, out.String()))
		})
	}
}

func TestTaskRowNeverEmitsEmptyString(t *testing.T) {
	for name, typ := range map[string]taskType{
		"none":   {},
		"grant":  {Grant: &taskGrantRevoke{}},
		"revoke": {Revoke: &taskGrantRevoke{}},
		"certify": {Certify: &struct {
			Outcome string `json:"outcome"`
		}{}},
	} {
		row := taskRow(taskSummary{ID: "t1", Type: typ})
		assertNoEmptyStrings(t, "taskRow "+name, []map[string]any{row})
	}
}

func TestServerCountRowNeverEmitsEmptyString(t *testing.T) {
	n := int64(0)
	for name, count := range map[string]*int64{"no tool-state": nil, "tool-state": &n} {
		row := serverCountRow(serverView{ConnectorID: "c1"}, count)
		assertNoEmptyStrings(t, "serverCountRow "+name, []map[string]any{row})
	}
}

func TestGatewayToolRowsNeverContainEmptyString(t *testing.T) {
	var out bytes.Buffer
	mcpGatewayListToolsCmd.SetOut(&out)
	t.Cleanup(func() { mcpGatewayListToolsCmd.SetOut(nil) })
	if err := emitGatewayTools(mcpGatewayListToolsCmd, []mcpgateway.Tool{{Name: "t1"}}, false, 0); err != nil {
		t.Fatal(err)
	}
	assertNoEmptyStrings(t, "mcp gateway list-tools", decodeNDJSONRows(t, out.String()))
}

func TestDocsRowsNeverContainEmptyString(t *testing.T) {
	t.Run("docs search", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, `[{"path":"/x"}]`)
		}))
		defer srv.Close()
		orig := mintlifySearchURL
		mintlifySearchURL = srv.URL
		t.Cleanup(func() { mintlifySearchURL = orig })
		var out bytes.Buffer
		resetCmdFlags(t, docsSearchCmd)
		docsSearchCmd.SetOut(&out)
		docsSearchCmd.SetContext(context.Background())
		if err := docsSearchCmd.RunE(docsSearchCmd, []string{"q"}); err != nil {
			t.Fatal(err)
		}
		assertNoEmptyStrings(t, "docs search", decodeNDJSONRows(t, out.String()))
	})
	t.Run("docs endpoints", func(t *testing.T) {
		primeOpenAPICacheWith(t, "paths:\n  /api/v1/x:\n    get: {}\n")
		var out bytes.Buffer
		resetCmdFlags(t, docsEndpointsCmd)
		docsEndpointsCmd.SetOut(&out)
		docsEndpointsCmd.SetContext(context.Background())
		if err := docsEndpointsCmd.RunE(docsEndpointsCmd, nil); err != nil {
			t.Fatal(err)
		}
		assertNoEmptyStrings(t, "docs endpoints", decodeNDJSONRows(t, out.String()))
	})
}

// The builders no command case above reaches, fed an object with every field
// absent.
func TestObjectRowBuildersNeverEmitEmptyString(t *testing.T) {
	builders := map[string]func(map[string]any) map[string]any{
		"findingRuleRow":               findingRuleRow,
		"findingTransformationRuleRow": findingTransformationRuleRow,
		"shadowMCPOccurrenceRow":       shadowMCPOccurrenceRow,
		"roleMiningSuggestionRow":      roleMiningSuggestionRow,
		"roleMiningRunRow":             roleMiningRunRow,
		"roleMiningAnalysisRow":        roleMiningAnalysisRow,
		"roleMiningUserRow":            roleMiningUserRow,
	}
	for name, build := range builders {
		assertNoEmptyStrings(t, name, []map[string]any{build(map[string]any{})})
	}
}

func TestOptStringPassesNonStringsThrough(t *testing.T) {
	item := map[string]any{"s": "v", "e": "", "n": 3.0, "b": false}
	for key, want := range map[string]any{"s": "v", "e": nil, "n": 3.0, "b": false, "missing": nil} {
		if got := optString(item, key); got != want {
			t.Errorf("optString(%q) = %v, want %v", key, got, want)
		}
	}
}
