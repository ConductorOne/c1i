package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/spf13/cobra"
)

// webhookReq is one request the stub server saw.
type webhookReq struct {
	method string
	path   string // escaped
	body   map[string]any
}

// webhooksServer records every request and answers each with respBody.
func webhooksServer(t *testing.T, respBody string) (*httptest.Server, *[]webhookReq) {
	t.Helper()
	var reqs []webhookReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body map[string]any
		if len(b) > 0 {
			if err := json.Unmarshal(b, &body); err != nil {
				t.Errorf("unmarshal body: %v (%s)", err, b)
			}
		}
		reqs = append(reqs, webhookReq{method: r.Method, path: r.URL.EscapedPath(), body: body})
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, respBody)
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs
}

// runWebhookCmd resets cmd's flags, applies flags, and runs it against srv.
func runWebhookCmd(t *testing.T, srv *httptest.Server, cmd *cobra.Command, args []string, flags map[string]string) (string, error) {
	t.Helper()
	resetCmdFlags(t, cmd)
	stubCatalogClient(t, srv)
	t.Setenv("C1I_URL", "https://example.invalid")
	for k, v := range flags {
		if err := cmd.Flags().Set(k, v); err != nil {
			t.Fatalf("set --%s=%q: %v", k, v, err)
		}
	}
	var out bytes.Buffer
	cmd.SetOut(&out)
	t.Cleanup(func() { cmd.SetOut(nil) })
	cmd.SetContext(context.Background())
	err := runEAfterBlankCheck(cmd, args)
	return out.String(), err
}

const webhookEnvelope = `{"webhook":{"id":"wh-1","displayName":"zz-webhook-a","url":"https://hooks.example.com/x"}}`

const webhookDoc = "{\n  \"id\": \"wh-1\",\n  \"displayName\": \"zz-webhook-a\",\n  \"url\": \"https://hooks.example.com/x\"\n}\n"

func TestWebhooksCreateSendsEveryWritableField(t *testing.T) {
	withCatalogDryRun(t, false)
	srv, reqs := webhooksServer(t, webhookEnvelope)
	_, err := runWebhookCmd(t, srv, webhooksCreateCmd, nil, map[string]string{
		"display-name":     "zz-webhook-a",
		"destination-url":  "https://hooks.example.com/x",
		"description":      "d1",
		"callback-timeout": "1m30s",
	})
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if len(*reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(*reqs))
	}
	got := (*reqs)[0]
	if got.method != http.MethodPost || got.path != "/api/v1/webhooks" {
		t.Errorf("request = %s %s, want POST /api/v1/webhooks", got.method, got.path)
	}
	want := map[string]any{
		"displayName":     "zz-webhook-a",
		"url":             "https://hooks.example.com/x",
		"description":     "d1",
		"callbackTimeout": "90s",
	}
	if !reflect.DeepEqual(got.body, want) {
		t.Errorf("body = %#v\nwant %#v", got.body, want)
	}
}

func TestWebhooksCreateOmitsUnsetOptionals(t *testing.T) {
	withCatalogDryRun(t, false)
	srv, reqs := webhooksServer(t, webhookEnvelope)
	if _, err := runWebhookCmd(t, srv, webhooksCreateCmd, nil, map[string]string{
		"display-name":    "zz-webhook-a",
		"destination-url": "https://hooks.example.com/x",
	}); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if len(*reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(*reqs))
	}
	want := map[string]any{"displayName": "zz-webhook-a", "url": "https://hooks.example.com/x"}
	if got := (*reqs)[0].body; !reflect.DeepEqual(got, want) {
		t.Errorf("body = %#v\nwant %#v", got, want)
	}
}

// A mutation prints the unwrapped response, never projected: a session-wide
// C1I_FIELDS that names nothing in it is ignored.
func TestWebhooksMutationsPrintUnprojected(t *testing.T) {
	withCatalogDryRun(t, false)
	cases := []struct {
		name  string
		cmd   *cobra.Command
		args  []string
		flags map[string]string
		resp  string
		want  string
	}{
		{"create", webhooksCreateCmd, nil, map[string]string{"display-name": "zz-webhook-a", "destination-url": "https://hooks.example.com/x"}, webhookEnvelope, webhookDoc},
		{"update", webhooksUpdateCmd, []string{"wh-1"}, map[string]string{"description": "d2"}, webhookEnvelope, webhookDoc},
		{"test", webhooksTestCmd, []string{"wh-1"}, nil, `{"webhook":{"id":"inst-1","webhookId":"wh-1","state":"WEBHOOK_STATE_PENDING"}}`,
			"{\n  \"id\": \"inst-1\",\n  \"webhookId\": \"wh-1\",\n  \"state\": \"WEBHOOK_STATE_PENDING\"\n}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setFieldsFlag(t, "no_such_field")
			srv, _ := webhooksServer(t, tc.resp)
			out, err := runWebhookCmd(t, srv, tc.cmd, tc.args, tc.flags)
			if err != nil {
				t.Fatalf("RunE: %v", err)
			}
			if out != tc.want {
				t.Errorf("stdout = %q\nwant the unwrapped response %q", out, tc.want)
			}
		})
	}
}

func TestWebhooksURLMustBeHTTPS(t *testing.T) {
	withCatalogDryRun(t, false)
	bad := []string{
		"http://hooks.example.com/x",
		"ftp://hooks.example.com/x",
		"hooks.example.com/x",
		"not a url",
		"https://",
		"https:///path-only",
		"",
		"   ",
	}
	for _, u := range bad {
		for _, tc := range []struct {
			name  string
			cmd   *cobra.Command
			args  []string
			flags map[string]string
		}{
			{"create", webhooksCreateCmd, nil, map[string]string{"display-name": "zz-webhook-a", "destination-url": u}},
			{"update", webhooksUpdateCmd, []string{"wh-1"}, map[string]string{"destination-url": u}},
		} {
			t.Run(tc.name+" "+u, func(t *testing.T) {
				srv, reqs := webhooksServer(t, webhookEnvelope)
				_, err := runWebhookCmd(t, srv, tc.cmd, tc.args, tc.flags)
				if code := exitCode(err); code != exitUsage {
					t.Errorf("exit = %d, want %d (err: %v)", code, exitUsage, err)
				}
				if len(*reqs) != 0 {
					t.Errorf("sent %d request(s) for a refused --destination-url", len(*reqs))
				}
			})
		}
	}
}

func TestWebhooksUsageErrorsSendNothing(t *testing.T) {
	withCatalogDryRun(t, false)
	cases := []struct {
		name  string
		cmd   *cobra.Command
		args  []string
		flags map[string]string
	}{
		{"create empty name", webhooksCreateCmd, nil, map[string]string{"display-name": "", "destination-url": "https://hooks.example.com/x"}},
		{"create whitespace name", webhooksCreateCmd, nil, map[string]string{"display-name": "  ", "destination-url": "https://hooks.example.com/x"}},
		{"create bad timeout", webhooksCreateCmd, nil, map[string]string{"display-name": "zz-webhook-a", "destination-url": "https://hooks.example.com/x", "callback-timeout": "soon"}},
		{"create negative timeout", webhooksCreateCmd, nil, map[string]string{"display-name": "zz-webhook-a", "destination-url": "https://hooks.example.com/x", "callback-timeout": "-5s"}},
		{"update nothing", webhooksUpdateCmd, []string{"wh-1"}, nil},
		{"update empty name", webhooksUpdateCmd, []string{"wh-1"}, map[string]string{"display-name": ""}},
		{"update whitespace name", webhooksUpdateCmd, []string{"wh-1"}, map[string]string{"display-name": " \t"}},
		{"update bad timeout", webhooksUpdateCmd, []string{"wh-1"}, map[string]string{"callback-timeout": "soon"}},
		{"update negative timeout", webhooksUpdateCmd, []string{"wh-1"}, map[string]string{"callback-timeout": "-1ns"}},
		{"update decimal timeout", webhooksUpdateCmd, []string{"wh-1"}, map[string]string{"callback-timeout": "1.5s"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, reqs := webhooksServer(t, webhookEnvelope)
			_, err := runWebhookCmd(t, srv, tc.cmd, tc.args, tc.flags)
			if code := exitCode(err); code != exitUsage {
				t.Errorf("exit = %d, want %d (err: %v)", code, exitUsage, err)
			}
			if len(*reqs) != 0 {
				t.Errorf("sent %d request(s)", len(*reqs))
			}
		})
	}
}

// Every changed flag is masked, an empty --description included (it clears).
func TestWebhooksUpdateMaskFromChangedFlags(t *testing.T) {
	withCatalogDryRun(t, false)
	const id = "wh/1 ?#"
	cases := []struct {
		name    string
		flags   map[string]string
		webhook map[string]any
		mask    string
	}{
		{
			name: "all fields",
			flags: map[string]string{
				"display-name":     "zz-webhook-b",
				"description":      "",
				"destination-url":  "https://hooks.example.com/y",
				"callback-timeout": "5m",
			},
			webhook: map[string]any{"id": id, "displayName": "zz-webhook-b", "description": "", "url": "https://hooks.example.com/y", "callbackTimeout": "300s"},
			mask:    "displayName,description,url,callbackTimeout",
		},
		{
			name:    "description only",
			flags:   map[string]string{"description": "d2"},
			webhook: map[string]any{"id": id, "description": "d2"},
			mask:    "description",
		},
		{
			name:    "zero timeout is a value",
			flags:   map[string]string{"callback-timeout": "0s"},
			webhook: map[string]any{"id": id, "callbackTimeout": "0s"},
			mask:    "callbackTimeout",
		},
		{
			name:    "sub-second timeout, whitespace trimmed",
			flags:   map[string]string{"callback-timeout": " 1s500ms "},
			webhook: map[string]any{"id": id, "callbackTimeout": "1.5s"},
			mask:    "callbackTimeout",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, reqs := webhooksServer(t, webhookEnvelope)
			if _, err := runWebhookCmd(t, srv, webhooksUpdateCmd, []string{id}, tc.flags); err != nil {
				t.Fatalf("RunE: %v", err)
			}
			if len(*reqs) != 1 {
				t.Fatalf("requests = %d, want 1", len(*reqs))
			}
			got := (*reqs)[0]
			if got.method != http.MethodPost || got.path != "/api/v1/webhooks/wh%2F1%20%3F%23" {
				t.Errorf("request = %s %s", got.method, got.path)
			}
			want := map[string]any{"webhook": tc.webhook, "updateMask": tc.mask}
			if !reflect.DeepEqual(got.body, want) {
				t.Errorf("body = %#v\nwant %#v", got.body, want)
			}
		})
	}
}

// Each id-addressed command escapes its positional into the exact path.
func TestWebhooksIDCommandsEscapePath(t *testing.T) {
	withCatalogDryRun(t, false)
	const id = "wh/1 ?#"
	cases := []struct {
		name   string
		cmd    *cobra.Command
		method string
		path   string
		body   map[string]any
	}{
		{"get", webhooksGetCmd, http.MethodGet, "/api/v1/webhooks/wh%2F1%20%3F%23", nil},
		{"delete", webhooksDeleteCmd, http.MethodDelete, "/api/v1/webhooks/wh%2F1%20%3F%23", nil},
		{"test", webhooksTestCmd, http.MethodPost, "/api/v1/webhooks/wh%2F1%20%3F%23/test", map[string]any{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, reqs := webhooksServer(t, webhookEnvelope)
			if _, err := runWebhookCmd(t, srv, tc.cmd, []string{id}, nil); err != nil {
				t.Fatalf("RunE: %v", err)
			}
			if len(*reqs) != 1 {
				t.Fatalf("requests = %d, want 1", len(*reqs))
			}
			got := (*reqs)[0]
			if got.method != tc.method || got.path != tc.path {
				t.Errorf("request = %s %s, want %s %s", got.method, got.path, tc.method, tc.path)
			}
			if tc.body != nil && !reflect.DeepEqual(got.body, tc.body) {
				t.Errorf("body = %#v, want %#v", got.body, tc.body)
			}
		})
	}
}

func TestWebhooksDeletePrintsAPIResponse(t *testing.T) {
	withCatalogDryRun(t, false)
	srv, _ := webhooksServer(t, `{}`)
	out, err := runWebhookCmd(t, srv, webhooksDeleteCmd, []string{"wh-1"}, nil)
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if out != "{}\n" {
		t.Errorf("output = %q, want the API's {}", out)
	}
}

func TestWebhooksDryRunSendsNothing(t *testing.T) {
	withCatalogDryRun(t, true)
	cases := []struct {
		name        string
		cmd         *cobra.Command
		args        []string
		flags       map[string]string
		method, url string
	}{
		{"create", webhooksCreateCmd, nil, map[string]string{"display-name": "zz-webhook-a", "destination-url": "https://hooks.example.com/x"}, "POST", "https://example.invalid/api/v1/webhooks"},
		{"update", webhooksUpdateCmd, []string{"wh 1"}, map[string]string{"description": "d2"}, "POST", "https://example.invalid/api/v1/webhooks/wh%201"},
		{"delete", webhooksDeleteCmd, []string{"wh 1"}, nil, "DELETE", "https://example.invalid/api/v1/webhooks/wh%201"},
		{"test", webhooksTestCmd, []string{"wh 1"}, nil, "POST", "https://example.invalid/api/v1/webhooks/wh%201/test"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, reqs := webhooksServer(t, webhookEnvelope)
			out, err := runWebhookCmd(t, srv, tc.cmd, tc.args, tc.flags)
			if err != nil {
				t.Fatalf("RunE: %v", err)
			}
			if len(*reqs) != 0 {
				t.Errorf("dry-run sent %d request(s)", len(*reqs))
			}
			pr := previewDoc(t, out)
			if len(pr) != 1 || pr[0]["method"] != tc.method || pr[0]["url"] != tc.url {
				t.Errorf("output = %q, want one %s %s", out, tc.method, tc.url)
			}
		})
	}
}

// Rows keep real JSON types, and absent values are null rather than "".
func TestWebhookRow(t *testing.T) {
	got := webhookRow(webhookListItem{
		ID:              "wh-1",
		DisplayName:     "zz-webhook-a",
		URL:             "https://hooks.example.com/x",
		CallbackTimeout: "300s",
		CreatedAt:       "2026-01-01T00:00:00Z",
	})
	want := map[string]any{
		"id":               "wh-1",
		"display_name":     "zz-webhook-a",
		"url":              "https://hooks.example.com/x",
		"description":      nil,
		"callback_timeout": "300s",
		"created_at":       "2026-01-01T00:00:00Z",
		"updated_at":       nil,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("row = %#v\nwant %#v", got, want)
	}
}
