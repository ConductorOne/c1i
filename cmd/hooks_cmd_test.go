package cmd

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// runPairs is runWebhookCmd with ordered flag pairs, so a repeatable flag can
// be set more than once.
func runPairs(t *testing.T, srv interface{ Close() }, cmd *cobra.Command, args []string, pairs ...string) (string, error) {
	t.Helper()
	resetCmdFlags(t, cmd)
	t.Setenv("C1I_URL", "https://example.invalid")
	for i := 0; i+1 < len(pairs); i += 2 {
		if err := cmd.Flags().Set(pairs[i], pairs[i+1]); err != nil {
			t.Fatalf("set --%s=%q: %v", pairs[i], pairs[i+1], err)
		}
	}
	var out bytes.Buffer
	restoreCmdWriters(t)
	cmd.SetOut(&out)
	t.Cleanup(func() { cmd.SetOut(nil) })
	cmd.SetContext(context.Background())
	err := runEAfterBlankCheck(cmd, args)
	return out.String(), err
}

// writes drops the pre-send lookups (GETs) from the recorded requests.
func hookWrites(reqs []webhookReq) []webhookReq {
	var out []webhookReq
	for _, r := range reqs {
		if r.method != http.MethodGet {
			out = append(out, r)
		}
	}
	return out
}

const hookEnvelope = `{"hook":{"id":"hk-1","displayName":"zz-hook"}}`

func TestHooksCreateBuildsFunctionHook(t *testing.T) {
	withCatalogDryRun(t, false)
	srv, reqs := webhooksServer(t, hookEnvelope)
	stubCatalogClient(t, srv)
	out, err := runPairs(t, srv, hooksCreateCmd, nil,
		"display-name", "zz-hook", "event", "post-tool-use", "function-id", "fn-1", "function-commit-id", "c-1",
		"filter-cel", `ctx.tool_name == "x"`, "priority", "7", "enabled", "false", "managed-by-guardrails", "true", "description", "")
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	want := map[string]any{
		"displayName": "zz-hook", "event": "HOOK_EVENT_TYPE_POST_TOOL_USE", "description": "",
		"function": map[string]any{"functionId": "fn-1", "commitId": "c-1"},
		"filter":   map[string]any{"celExpression": `ctx.tool_name == "x"`},
		"priority": float64(7), "enabled": false, "managedByGuardrails": true,
	}
	if w := hookWrites(*reqs); len(w) != 1 || w[0].method != http.MethodPost || w[0].path != "/api/v1/hooks" || !reflect.DeepEqual(w[0].body, want) {
		t.Fatalf("requests = %#v\nwant POST /api/v1/hooks %#v", *reqs, want)
	}
	if !strings.Contains(out, `"id": "hk-1"`) || strings.Contains(out, `"hook"`) {
		t.Errorf("stdout is not the unwrapped hook: %s", out)
	}
}

func TestHooksCreateRefusals(t *testing.T) {
	withCatalogDryRun(t, false)
	file := filepath.Join(t.TempDir(), "h.json")
	if err := os.WriteFile(file, []byte(`{"displayName":"zz"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string][]string{
		"no function":    {"display-name", "zz", "event", "pre-output"},
		"no event":       {"display-name", "zz", "function-id", "f"},
		"bad event":      {"display-name", "zz", "event", "nope", "function-id", "f"},
		"body plus flag": {"body-file", file, "priority", "1"},
		"commit only":    {"display-name", "zz", "event", "pre-output", "function-commit-id", "c"},
	}
	for name, pairs := range cases {
		t.Run(name, func(t *testing.T) {
			srv, reqs := webhooksServer(t, hookEnvelope)
			stubCatalogClient(t, srv)
			_, err := runPairs(t, srv, hooksCreateCmd, nil, pairs...)
			if code := exitCode(err); code != exitUsage {
				t.Errorf("exit = %d, want %d (err %v)", code, exitUsage, err)
			}
			if len(*reqs) != 0 {
				t.Errorf("sent %d request(s)", len(*reqs))
			}
		})
	}
}

func TestHooksCreateBodyFileIsSentVerbatim(t *testing.T) {
	withCatalogDryRun(t, false)
	file := filepath.Join(t.TempDir(), "h.json")
	raw := `{"displayName":"zz","event":"HOOK_EVENT_TYPE_PRE_OUTPUT","builtinPattern":{"linkFilter":{"blockImages":true}}}`
	if err := os.WriteFile(file, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	srv, reqs := webhooksServer(t, hookEnvelope)
	stubCatalogClient(t, srv)
	if _, err := runPairs(t, srv, hooksCreateCmd, nil, "body-file", file); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	want := map[string]any{"displayName": "zz", "event": "HOOK_EVENT_TYPE_PRE_OUTPUT",
		"builtinPattern": map[string]any{"linkFilter": map[string]any{"blockImages": true}}}
	if len(*reqs) != 1 || !reflect.DeepEqual((*reqs)[0].body, want) {
		t.Fatalf("requests = %#v, want body %#v", *reqs, want)
	}
}

func TestHooksUpdateMask(t *testing.T) {
	withCatalogDryRun(t, false)
	cases := []struct {
		name  string
		pairs []string
		hook  map[string]any
		mask  string
	}{
		{"clear filter and priority", []string{"priority", "0", "clear-filter", "true"},
			map[string]any{"id": "hk-1", "priority": float64(0), "filter": nil}, "priority,filter"},
		{"function with commit", []string{"function-id", "fn-2", "function-commit-id", "c-9"},
			map[string]any{"id": "hk-1", "function": map[string]any{"functionId": "fn-2", "commitId": "c-9"}}, "function"},
		{"function only", []string{"function-id", "fn-2"},
			map[string]any{"id": "hk-1", "function": map[string]any{"functionId": "fn-2"}}, "function"},
		{"filter", []string{"filter-cel", "true"},
			map[string]any{"id": "hk-1", "filter": map[string]any{"celExpression": "true"}}, "filter"},
		{"enabled false", []string{"enabled", "false"}, map[string]any{"id": "hk-1", "enabled": false}, "enabled"},
		{"empty description", []string{"description", ""}, map[string]any{"id": "hk-1", "description": ""}, "description"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, reqs := webhooksServer(t, hookEnvelope)
			stubCatalogClient(t, srv)
			if _, err := runPairs(t, srv, hooksUpdateCmd, []string{"hk-1"}, tc.pairs...); err != nil {
				t.Fatalf("RunE: %v", err)
			}
			want := map[string]any{"hook": tc.hook, "updateMask": tc.mask}
			if w := hookWrites(*reqs); len(w) != 1 || w[0].path != "/api/v1/hooks/hk-1" || !reflect.DeepEqual(w[0].body, want) {
				t.Fatalf("requests = %#v\nwant POST /api/v1/hooks/hk-1 %#v", *reqs, want)
			}
		})
	}
}

func TestHooksUpdateRefusals(t *testing.T) {
	withCatalogDryRun(t, false)
	cases := map[string][]string{
		"nothing":            nil,
		"clear plus set":     {"clear-filter", "true", "filter-cel", "true"},
		"commit without id":  {"function-commit-id", "c"},
		"mask without body":  {"update-mask", "description"},
		"blank display name": {"display-name", " "},
	}
	for name, pairs := range cases {
		t.Run(name, func(t *testing.T) {
			srv, reqs := webhooksServer(t, hookEnvelope)
			stubCatalogClient(t, srv)
			_, err := runPairs(t, srv, hooksUpdateCmd, []string{"hk-1"}, pairs...)
			if code := exitCode(err); code != exitUsage {
				t.Errorf("exit = %d, want %d (err %v)", code, exitUsage, err)
			}
			if len(*reqs) != 0 {
				t.Errorf("sent %d request(s)", len(*reqs))
			}
		})
	}
}

func TestHooksDeleteSendsDelete(t *testing.T) {
	withCatalogDryRun(t, false)
	srv, reqs := webhooksServer(t, `{}`)
	stubCatalogClient(t, srv)
	if _, err := runPairs(t, srv, hooksDeleteCmd, []string{"hk-1"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if len(*reqs) != 1 || (*reqs)[0].method != http.MethodDelete || (*reqs)[0].path != "/api/v1/hooks/hk-1" {
		t.Fatalf("requests = %#v", *reqs)
	}
}

func TestHookRowKeepsTypesAndKind(t *testing.T) {
	row := hookRow(map[string]any{"id": "h", "displayName": "n", "description": "", "event": "E", "enabled": false,
		"managedByGuardrails": true, "priority": float64(5), "builtinPattern": map[string]any{"piiRedaction": map[string]any{}}})
	if row["enabled"] != false || row["managed_by_guardrails"] != true || row["priority"] != float64(5) {
		t.Errorf("bool/number values were not kept as is: %#v", row)
	}
	if row["kind"] != "builtin_pattern" || row["description"] != nil {
		t.Errorf("kind/description = %v/%v", row["kind"], row["description"])
	}
	if hookKind(map[string]any{"function": map[string]any{"functionId": "f"}}) != "function" || hookKind(map[string]any{}) != nil {
		t.Error("hookKind misclassified")
	}
}

func TestWebhooksSearchBody(t *testing.T) {
	withCatalogDryRun(t, false)
	srv, reqs := webhooksServer(t, `{"list":[{"id":"wh-1","displayName":"zz"}],"nextPageToken":""}`)
	orig := newListClient
	newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newListClient = orig })
	out, err := runPairs(t, srv, webhooksSearchCmd, nil, "query", "zz", "webhook-id", "wh-1", "webhook-id", "wh-2")
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if len(*reqs) != 1 || (*reqs)[0].path != "/api/v1/search/webhooks" {
		t.Fatalf("requests = %#v", *reqs)
	}
	b := (*reqs)[0].body
	refs := []any{map[string]any{"id": "wh-1"}, map[string]any{"id": "wh-2"}}
	if b["query"] != "zz" || !reflect.DeepEqual(b["refs"], refs) {
		t.Errorf("body = %#v, want query zz and refs %v", b, refs)
	}
	if !strings.Contains(out, `"id":"wh-1"`) {
		t.Errorf("row missing: %s", out)
	}
}

func TestSetDelegationBody(t *testing.T) {
	withCatalogDryRun(t, false)
	cases := []struct {
		name  string
		pairs []string
		body  map[string]any
	}{
		{"set", []string{"delegate-user-id", "u-2", "start-at", "2026-11-19T12:00:00Z", "expire-at", "2026-11-20T12:00:00Z"},
			map[string]any{"delegatedUserId": "u-2", "delegationStartAt": "2026-11-19T12:00:00Z", "delegationExpireAt": "2026-11-20T12:00:00Z"}},
		{"set without window", []string{"delegate-user-id", "u-2"}, map[string]any{"delegatedUserId": "u-2"}},
		{"clear", []string{"clear", "true"}, map[string]any{"delegatedUserId": ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, reqs := webhooksServer(t, `{"item":{"userId":"u-1"}}`)
			stubCatalogClient(t, srv)
			if _, err := runPairs(t, srv, usersSetDelegationCmd, []string{"u-1"}, tc.pairs...); err != nil {
				t.Fatalf("RunE: %v", err)
			}
			if len(*reqs) != 1 || (*reqs)[0].path != "/api/v1/users/u-1/set-delegation-by-admin" || !reflect.DeepEqual((*reqs)[0].body, tc.body) {
				t.Fatalf("requests = %#v, want body %#v", *reqs, tc.body)
			}
		})
	}
	for name, pairs := range map[string][]string{
		"neither":         nil,
		"clear plus user": {"clear", "true", "delegate-user-id", "u-2"},
		"clear plus time": {"clear", "true", "expire-at", "2026-11-20T12:00:00Z"},
		"bad time":        {"delegate-user-id", "u-2", "start-at", "tomorrow"},
	} {
		t.Run("refuse "+name, func(t *testing.T) {
			srv, reqs := webhooksServer(t, `{}`)
			stubCatalogClient(t, srv)
			_, err := runPairs(t, srv, usersSetDelegationCmd, []string{"u-1"}, pairs...)
			if code := exitCode(err); code != exitUsage || len(*reqs) != 0 {
				t.Errorf("exit %d, %d request(s), err %v; want exit 2 and none", code, len(*reqs), err)
			}
		})
	}
}

func TestTestProvisionJoinsWordsAndMapsServerErrors(t *testing.T) {
	withCatalogDryRun(t, false)
	srv, reqs := webhooksServer(t, `{"value":"3","type":"number"}`)
	stubCatalogClient(t, srv)
	out, err := runPairs(t, srv, policiesTestProvisionCmd, []string{"1", "+", "2"})
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if len(*reqs) != 1 || (*reqs)[0].path != "/api/v1/policies/test-account-provision-policy" || (*reqs)[0].body["cel"] != "1 + 2" {
		t.Fatalf("requests = %#v", *reqs)
	}
	if !strings.Contains(out, `"value": "3"`) {
		t.Errorf("stdout = %s", out)
	}
}

func TestUsersProfileTypesGet(t *testing.T) {
	srv, reqs := webhooksServer(t, `{"profileTypes":[]}`)
	stubCatalogClient(t, srv)
	out, err := runPairs(t, srv, usersProfileTypesCmd, []string{"a/b"})
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if len(*reqs) != 1 || (*reqs)[0].method != http.MethodGet || (*reqs)[0].path != "/api/v1/users/a%2Fb/profile-types" {
		t.Fatalf("requests = %#v", *reqs)
	}
	if !strings.Contains(out, `"profileTypes": []`) {
		t.Errorf("stdout = %s", out)
	}
}

// A create without --enabled sends no enabled key, and an update without it
// leaves it out of the body and the mask: an enabled hook intercepts tool calls.
func TestHooksNeverSendEnabledUnlessAsked(t *testing.T) {
	withCatalogDryRun(t, false)
	srv, reqs := webhooksServer(t, hookEnvelope)
	stubCatalogClient(t, srv)
	if _, err := runPairs(t, srv, hooksCreateCmd, nil, "display-name", "zz", "event", "pre-output", "function-id", "fn-1"); err != nil {
		t.Fatalf("create: %v", err)
	}
	// reqs holds the pre-read GET of the function, then the write.
	last := hookWrites(*reqs)[0]
	want := map[string]any{"displayName": "zz", "event": "HOOK_EVENT_TYPE_PRE_OUTPUT", "function": map[string]any{"functionId": "fn-1"}}
	if last.method != http.MethodPost || !reflect.DeepEqual(last.body, want) {
		t.Errorf("create body = %#v, want exactly %#v", last.body, want)
	}
	*reqs = nil
	if _, err := runPairs(t, srv, hooksUpdateCmd, []string{"hk-1"}, "priority", "2"); err != nil {
		t.Fatalf("update: %v", err)
	}
	hook, _ := hookWrites(*reqs)[0].body["hook"].(map[string]any)
	mask, _ := hookWrites(*reqs)[0].body["updateMask"].(string)
	if _, has := hook["enabled"]; has || strings.Contains(mask, "enabled") {
		t.Errorf("update sent enabled: hook %#v mask %q", hook, mask)
	}
}

func TestHooksFunctionRefsAreLookedUpFirst(t *testing.T) {
	withCatalogDryRun(t, false)
	for _, dry := range []bool{false, true} {
		for _, tc := range []struct {
			name         string
			pairs        []string
			notFoundPath string
		}{
			{"unknown function", []string{"function-id", "fn-x"}, "/api/v1/functions/fn-x"},
			{"unknown commit", []string{"function-id", "fn-1", "function-commit-id", "c-x"}, "/api/v1/functions/fn-1/commits/c-x"},
		} {
			for _, create := range []bool{true, false} {
				t.Run(tc.name+map[bool]string{true: " create", false: " update"}[create]+map[bool]string{true: " dry", false: ""}[dry], func(t *testing.T) {
					srv, reqs := hookRefServer(t, tc.notFoundPath)
					stubCatalogClient(t, srv)
					cmd, args, pairs := hooksUpdateCmd, []string{"hk-1"}, tc.pairs
					if create {
						cmd, args = hooksCreateCmd, nil
						pairs = append([]string{"display-name", "zz", "event", "pre-output"}, tc.pairs...)
					}
					withCatalogDryRun(t, dry)
					_, err := runPairs(t, srv, cmd, args, pairs...)
					if code := exitCode(err); code != exitNotFound {
						t.Errorf("exit = %d, want %d (err %v)", code, exitNotFound, err)
					}
					for _, r := range *reqs {
						if r.method != http.MethodGet {
							t.Errorf("sent a %s %s after a failed lookup", r.method, r.path)
						}
					}
				})
			}
		}
	}
}

func hookRefServer(t *testing.T, notFound string) (*httptest.Server, *[]webhookReq) {
	t.Helper()
	var reqs []webhookReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs = append(reqs, webhookReq{method: r.Method, path: r.URL.EscapedPath()})
		w.Header().Set("Content-Type", "application/json")
		if r.URL.EscapedPath() == notFound {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"code":5,"message":"not found"}`)
			return
		}
		_, _ = io.WriteString(w, `{"hook":{"id":"hk-1"}}`)
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs
}

// Create is validated by the server in [0,1000]; Update is not, so c1i
// refuses an out-of-range --priority before the send.
func TestHooksPriorityRangeIsChecked(t *testing.T) {
	withCatalogDryRun(t, false)
	srv, reqs := webhooksServer(t, hookEnvelope)
	stubCatalogClient(t, srv)
	for _, bad := range []string{"-1", "1001"} {
		if _, err := runPairs(t, srv, hooksUpdateCmd, []string{"hk-1"}, "priority", bad); err == nil || !strings.Contains(err.Error(), "--priority must be between 0 and 1000") {
			t.Errorf("priority %s: err %v", bad, err)
		}
		if _, err := runPairs(t, srv, hooksCreateCmd, nil, "display-name", "zz", "event", "pre-output", "function-id", "fn-1", "priority", bad); err == nil || !strings.Contains(err.Error(), "--priority must be between 0 and 1000") {
			t.Errorf("create priority %s: err %v", bad, err)
		}
	}
	if sent := hookWrites(*reqs); len(sent) != 0 {
		t.Errorf("sent %v", sent)
	}
	if _, err := runPairs(t, srv, hooksUpdateCmd, []string{"hk-1"}, "priority", "1000"); err != nil {
		t.Errorf("priority 1000: %v", err)
	}
}
