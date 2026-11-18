package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// TestItemRowsGrantStatusOnlyWhenComputed: Search fills entitlementActive and
// grantCount only with includeGrantStatus, so a row must omit them otherwise
// (an uncomputed false/0 reads as real), and keep real types when present.
func TestItemRowsGrantStatusOnlyWhenComputed(t *testing.T) {
	var r resourceView
	if err := json.Unmarshal([]byte(`{"id":"r1","entitlementActive":true,"grantCount":"7"}`), &r); err != nil {
		t.Fatal(err)
	}
	var p promptView
	if err := json.Unmarshal([]byte(`{"id":"p1","entitlementActive":true,"grantCount":"7","arguments":[{"name":"topic"},{"name":"tone"}]}`), &p); err != nil {
		t.Fatal(err)
	}
	for name, row := range map[string]func(bool) map[string]any{
		"resource": func(g bool) map[string]any { return resourceRow(r, g) },
		"prompt":   func(g bool) map[string]any { return promptRow(p, g) },
	} {
		off := row(false)
		for _, k := range []string{"entitlement_active", "grant_count"} {
			if _, ok := off[k]; ok {
				t.Errorf("%s row without grant status has %q", name, k)
			}
		}
		on := row(true)
		if v, ok := on["entitlement_active"].(bool); !ok || !v {
			t.Errorf("%s entitlement_active = %#v, want bool true", name, on["entitlement_active"])
		}
		if v, ok := on["grant_count"].(int64); !ok || v != 7 {
			t.Errorf("%s grant_count = %#v, want int64 7", name, on["grant_count"])
		}
	}
	if got := promptRow(p, false)["argument_names"]; !reflect.DeepEqual(got, []string{"topic", "tone"}) {
		t.Errorf("argument_names = %#v", got)
	}
	if got := promptRow(promptView{ID: "p2"}, false)["argument_names"]; !reflect.DeepEqual(got, []string{}) {
		t.Errorf("argument_names with no arguments = %#v, want []", got)
	}
}

// runItemSearch runs a search command against a fake server and returns the
// request body it sent and the rows it printed.
func runItemSearch(t *testing.T, c *cobra.Command, resp string, args ...string) (map[string]any, []map[string]any) {
	t.Helper()
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("body %q: %v", raw, err)
		}
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	original := newListClient
	newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newListClient = original })
	t.Setenv("C1I_URL", "https://example.invalid")
	resetCmdFlags(t, rootCmd)
	resetCmds(t, c)
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil) })
	rootCmd.SetArgs(append([]string{"mcp", c.Parent().Name(), "search", "--app-id", "a1", "--connector-id", "c1"}, args...))
	if err := executeRootCtxOrFail(t); err != nil {
		t.Fatalf("search: %v", err)
	}
	var rows []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("row %q: %v", line, err)
		}
		rows = append(rows, row)
	}
	return body, rows
}

func executeRootCtxOrFail(t *testing.T) error {
	t.Helper()
	_, err := executeRootCtx(context.Background())
	return err
}

func TestItemSearchSendsFilters(t *testing.T) {
	const page = `{"list":[{"id":"x1","entitlementActive":false,"grantCount":"3"}],"nextPageToken":""}`

	body, rows := runItemSearch(t, mcpPromptsSearchCmd, page,
		"--query", "sum", "--state", "removed", "--state", "approved", "--include-grant-status")
	want := map[string]any{
		"appId": "a1", "connectorId": "c1", "query": "sum", "includeGrantStatus": true,
		"stateFilter": []any{"MCP_PROMPT_STATE_REMOVED", "MCP_PROMPT_STATE_APPROVED"},
	}
	if !reflect.DeepEqual(withoutPaging(body), want) {
		t.Errorf("prompts search body = %v, want %v", body, want)
	}
	if v, ok := rows[0]["grant_count"].(float64); !ok || v != 3 {
		t.Errorf("grant_count = %#v, want number 3", rows[0]["grant_count"])
	}

	body, rows = runItemSearch(t, mcpResourcesSearchCmd, page, "--kind", "template")
	want = map[string]any{"appId": "a1", "connectorId": "c1", "kindFilter": []any{"MCP_RESOURCE_KIND_TEMPLATE"}}
	if !reflect.DeepEqual(withoutPaging(body), want) {
		t.Errorf("resources search body = %v, want %v", body, want)
	}
	if _, ok := rows[0]["grant_count"]; ok {
		t.Errorf("grant_count present without --include-grant-status: %v", rows[0])
	}
}

func withoutPaging(body map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range body {
		if k != "pageSize" && k != "pageToken" {
			out[k] = v
		}
	}
	return out
}

// TestItemApproveWire pins each kind's Update request: its own route segment
// and envelope, mask "state", and approved by default.
func TestItemApproveWire(t *testing.T) {
	for _, tc := range []struct {
		group, segment, envelope, state string
		extra                           []string
	}{
		{"prompts", "mcp_prompts", "prompt", "MCP_PROMPT_STATE_APPROVED", nil},
		{"resources", "mcp_resources", "resource", "MCP_RESOURCE_STATE_APPROVED", nil},
		{"resources", "mcp_resources", "resource", "MCP_RESOURCE_STATE_DISABLED", []string{"--state", "disabled"}},
		{"prompts", "mcp_prompts", "prompt", "MCP_PROMPT_STATE_PENDING_REVIEW", []string{"--state", "pending"}},
	} {
		doc, err := previewItemApprove(t, tc.group, tc.extra...)
		if err != nil {
			t.Fatalf("%s %v: %v", tc.group, tc.extra, err)
		}
		if len(doc.Requests) != 1 {
			t.Fatalf("%s: %d requests", tc.group, len(doc.Requests))
		}
		req := doc.Requests[0]
		wantURL := "https://acme.conductor.one/api/v1/apps/app-x/connectors/conn-y/" + tc.segment + "/id-1"
		wantBody := map[string]any{
			tc.envelope:  map[string]any{"id": "id-1", "appId": "app-x", "connectorId": "conn-y", "state": tc.state},
			"updateMask": "state",
		}
		if req.Method != http.MethodPost || req.URL != wantURL || !reflect.DeepEqual(req.Body, wantBody) {
			t.Errorf("%s %v: got %s %s %v, want POST %s %v", tc.group, tc.extra, req.Method, req.URL, req.Body, wantURL, wantBody)
		}
	}
}

// TestItemApproveRefusesRemoved: REMOVED is sync's signal; the refusal is a
// usage error before anything is sent, even as a preview.
func TestItemApproveRefusesRemoved(t *testing.T) {
	for _, group := range []string{"prompts", "resources"} {
		doc, err := previewItemApprove(t, group, "--state", "removed")
		var ue *usageError
		if !errors.As(err, &ue) || !strings.Contains(err.Error(), "system-managed by sync") {
			t.Errorf("%s approve --state removed: err = %v, want usage error", group, err)
		}
		if len(doc.Requests) != 0 {
			t.Errorf("%s approve --state removed previewed %d requests", group, len(doc.Requests))
		}
	}
}

type itemPreview struct {
	Requests []struct {
		Method string         `json:"method"`
		URL    string         `json:"url"`
		Body   map[string]any `json:"body"`
	} `json:"requests"`
}

func previewItemApprove(t *testing.T, group string, extra ...string) (itemPreview, error) {
	t.Helper()
	resetCmdFlags(t, rootCmd)
	resetCmds(t, map[string]*cobra.Command{"prompts": mcpPromptsApproveCmd, "resources": mcpResourcesApproveCmd}[group])
	t.Setenv("C1I_URL", "")
	withDryRun(t)
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil) })
	rootCmd.SetArgs(append([]string{"mcp", group, "approve", "id-1",
		"--app-id", "app-x", "--connector-id", "conn-y", "--dry-run", "--url", "acme.conductor.one"}, extra...))
	err := executeRoot()
	var doc itemPreview
	if out.Len() > 0 {
		if jerr := json.Unmarshal(out.Bytes(), &doc); jerr != nil {
			t.Fatalf("preview: %v\n%s", jerr, out.String())
		}
	}
	return doc, err
}
