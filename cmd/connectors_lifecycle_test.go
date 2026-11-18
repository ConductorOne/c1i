package cmd

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const (
	connEnvConfigGet = `{"connectorView":{"connector":{"id":"cn1","config":{"@type":"type.googleapis.com/c1.api.app.v1.EnvConfig","configuration":{}}}},"expanded":[]}`
	connMCPConfigGet = `{"connectorView":{"connector":{"id":"cn1","config":{"@type":"type.googleapis.com/c1.models.ai_governance.v1.ExternalHostedMCPConfig"}}},"expanded":[]}`
)

// connRoute answers GETs with get and every write with {}.
func connRoute(getStatus int, get string) func(*http.Request, int) mutReply {
	return func(r *http.Request, _ int) mutReply {
		if r.Method == http.MethodGet {
			return mutReply{status: getStatus, body: get}
		}
		return mutReply{body: `{}`}
	}
}

func writesIn(reqs []entOwnerRequest) []entOwnerRequest {
	var out []entOwnerRequest
	for _, r := range reqs {
		if r.method != http.MethodGet {
			out = append(out, r)
		}
	}
	return out
}

func TestConnectorsDeletePreRead(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		get       string
		dry       bool
		wantExit  int
		wantWrite bool
		wantErr   string
	}{
		{name: "connector", status: 200, get: connEnvConfigGet, wantExit: 0, wantWrite: true},
		{name: "mcp connector", status: 200, get: connMCPConfigGet, wantExit: exitUsage, wantErr: "mcp servers delete"},
		{name: "mcp connector dry-run", status: 200, get: connMCPConfigGet, dry: true, wantExit: exitUsage, wantErr: "mcp servers delete"},
		{name: "unknown id", status: 404, get: `{"code":5,"message":"not found"}`, wantExit: exitNotFound},
		{name: "unknown id dry-run", status: 404, get: `{"code":5,"message":"not found"}`, dry: true, wantExit: exitNotFound},
		{name: "unrecognised read", status: 200, get: `{"something":{}}`, wantExit: exitServer, wantErr: "connectorView.connector"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, connRoute(tc.status, tc.get))
			args := []string{"connectors", "delete", "cn1", "--app-id", "a1"}
			if tc.dry {
				args = append(args, "--dry-run")
			}
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.exit != tc.wantExit {
				t.Fatalf("exit %d (err %v), want %d", r.exit, r.err, tc.wantExit)
			}
			if tc.wantErr != "" && (r.err == nil || !strings.Contains(r.err.Error(), tc.wantErr)) {
				t.Errorf("err %v, want it to mention %q", r.err, tc.wantErr)
			}
			reqs := s.requests()
			if len(reqs) == 0 || reqs[0].method != http.MethodGet || reqs[0].rawPath != "/api/v1/apps/a1/connectors/cn1" {
				t.Errorf("first request %v, want the connector GET", reqs)
			}
			if got := len(writesIn(reqs)) > 0; got != tc.wantWrite {
				t.Errorf("write sent = %v, want %v (%v)", got, tc.wantWrite, reqs)
			}
		})
	}
}

func TestConnectorsSyncStatusUnwrapsStatus(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, fixedReply(200, `{"status":{"state":"SYNC_STATE_DONE","lastError":""},"expanded":[]}`))
	r := runMutationCLI(t, s.srv, 0, nil, "connectors", "sync-status", "cn1", "--app-id", "a1")
	if r.exit != 0 {
		t.Fatalf("exit %d (err %v), want 0", r.exit, r.err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &got); err != nil {
		t.Fatalf("output is not one JSON object: %v (%q)", err, r.stdout)
	}
	if got["state"] != "SYNC_STATE_DONE" {
		t.Errorf("state = %v, want SYNC_STATE_DONE (%q)", got["state"], r.stdout)
	}
	if _, ok := got["status"]; ok {
		t.Errorf("output still has the status envelope: %q", r.stdout)
	}
	reqs := s.requests()
	if len(reqs) != 1 || reqs[0].method != http.MethodGet || reqs[0].rawPath != "/api/v1/apps/a1/connectors/cn1/sync_diagnostics/status" {
		t.Errorf("requests %v, want the sync-status GET", reqs)
	}
}

func TestConnectorsScheduleBody(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		wantExit int
		want     string
	}{
		{name: "cron and timezone", args: []string{"--cron", "0 3 * * *", "--timezone", "America/Chicago"}, want: `{"cron":{"cronSpec":"0 3 * * *","timezone":"America/Chicago"}}`},
		{name: "cron only", args: []string{"--cron", "0 3 * * *"}, want: `{"cron":{"cronSpec":"0 3 * * *"}}`},
		{name: "clear", args: []string{"--clear"}, want: `{}`},
		{name: "justification", args: []string{"--clear", "--justification", "why"}, want: `{"justification":{"reason":"why"}}`},
		{name: "clear with cron", args: []string{"--clear", "--cron", "0 3 * * *"}, wantExit: exitUsage},
		{name: "clear with timezone", args: []string{"--clear", "--timezone", "UTC"}, wantExit: exitUsage},
		{name: "timezone alone", args: []string{"--timezone", "UTC"}, wantExit: exitUsage},
		{name: "nothing", wantExit: exitUsage},
		{name: "blank cron", args: []string{"--cron", " "}, wantExit: exitUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, fixedReply(200, `{}`))
			args := append([]string{"connectors", "schedule", "cn1", "--app-id", "a1"}, tc.args...)
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.exit != tc.wantExit {
				t.Fatalf("exit %d (err %v), want %d", r.exit, r.err, tc.wantExit)
			}
			reqs := s.requests()
			if tc.wantExit != 0 {
				if len(reqs) != 0 {
					t.Errorf("sent %v, want nothing", reqs)
				}
				return
			}
			if len(reqs) != 1 || reqs[0].rawPath != "/api/v1/apps/a1/connectors/cn1/schedule" {
				t.Fatalf("requests %v", reqs)
			}
			assertJSONEqual(t, reqs[0].body, tc.want)
		})
	}
}

func TestConnectorsUpdateBody(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, fixedReply(200, `{"connectorView":{"connector":{"id":"cn1"}},"expanded":[]}`))
	file := writeTempFile(t, `{"catalogId":"cat1","config":{"@type":"t"}}`)
	r := runMutationCLI(t, s.srv, 0, nil, "connectors", "update", "cn1", "--app-id", "a1",
		"--body-file", file, "--update-mask", "config", "--justification", "why")
	if r.exit != 0 {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
	reqs := s.requests()
	if len(reqs) != 1 || reqs[0].method != http.MethodPost || reqs[0].rawPath != "/api/v1/apps/a1/connectors/cn1" {
		t.Fatalf("requests %v", reqs)
	}
	assertJSONEqual(t, reqs[0].body, `{"connector":{"id":"cn1","catalogId":"cat1","config":{"@type":"t"}},"updateMask":"config","justification":{"reason":"why"}}`)

	r = runMutationCLI(t, s.srv, 0, nil, "connectors", "update", "cn1", "--app-id", "a1", "--update-mask", "config")
	if r.exit != exitUsage || !strings.Contains(r.err.Error(), "pass --body-file with --update-mask") || strings.Contains(r.err.Error(), "derived") {
		t.Errorf("mask without body: exit %d, err %v; want exit 2 naming --body-file with --update-mask", r.exit, r.err)
	}

	r = runMutationCLI(t, s.srv, 0, nil, "connectors", "update", "cn1", "--app-id", "a1")
	if r.exit != exitUsage || !strings.Contains(r.err.Error(), "--body-file with --update-mask") {
		t.Errorf("no body: exit %d, err %v; want exit 2 naming --body-file", r.exit, r.err)
	}
}

func TestConnectorsCreateBody(t *testing.T) {
	userOK := func(r *http.Request, _ int) mutReply {
		if r.Method == http.MethodGet {
			if strings.Contains(r.URL.Path, "/users/missing") {
				return mutReply{status: 404, body: `{"code":5,"message":"not found"}`}
			}
			return mutReply{body: `{"user":{"id":"u1"}}`}
		}
		return mutReply{body: `{"connectorView":{"connector":{"id":"cn1"}},"expanded":[]}`}
	}
	for _, tc := range []struct {
		name      string
		args      []string
		wantExit  int
		wantWrite string
	}{
		{name: "flags", args: []string{"--catalog-id", "cat1", "--display-name", "zz", "--user-id", "u1"},
			wantWrite: `{"catalogId":"cat1","displayName":"zz","userIds":["u1"]}`},
		{name: "no catalog", args: []string{"--display-name", "zz"}, wantExit: exitUsage},
		{name: "unknown user flag", args: []string{"--catalog-id", "cat1", "--user-id", "missing"}, wantExit: exitNotFound},
		{name: "body file", args: []string{"--body-file", `@{"catalogId":"cat1","userIds":["u1"]}`},
			wantWrite: `{"catalogId":"cat1","userIds":["u1"]}`},
		{name: "body file unknown user", args: []string{"--body-file", `@{"catalogId":"cat1","userIds":["missing"]}`}, wantExit: exitNotFound},
		{name: "body file unknown user dry-run", args: []string{"--dry-run", "--body-file", `@{"catalogId":"cat1","userIds":["missing"]}`}, wantExit: exitNotFound},
		{name: "body file bad userIds", args: []string{"--body-file", `@{"catalogId":"cat1","userIds":"u1"}`}, wantExit: exitUsage},
		{name: "body file blank user", args: []string{"--body-file", `@{"catalogId":"cat1","userIds":[" "]}`}, wantExit: exitUsage},
		{name: "body file with flag", args: []string{"--body-file", `@{"catalogId":"cat1"}`, "--display-name", "zz"}, wantExit: exitUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, userOK)
			args := []string{"connectors", "create", "--app-id", "a1"}
			for _, a := range tc.args {
				if strings.HasPrefix(a, "@") {
					a = writeTempFile(t, a[1:])
				}
				args = append(args, a)
			}
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.exit != tc.wantExit {
				t.Fatalf("exit %d (err %v), want %d", r.exit, r.err, tc.wantExit)
			}
			writes := writesIn(s.requests())
			if tc.wantWrite == "" {
				if len(writes) != 0 {
					t.Errorf("sent %v, want no write", writes)
				}
				return
			}
			if len(writes) != 1 || writes[0].rawPath != "/api/v1/apps/a1/connectors" {
				t.Fatalf("writes %v", writes)
			}
			assertJSONEqual(t, writes[0].body, tc.wantWrite)
		})
	}
}

func TestConnectorCredentialsRotateBody(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, fixedReply(200, `{"credential":{"id":"cr-1","clientSecret":"s3"}}`))
	r := runMutationCLI(t, s.srv, 0, nil, "connectors", "credentials", "rotate", "--app-id", "a1", "--connector-id", "cn1")
	if r.exit != 0 {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
	reqs := s.requests()
	if len(reqs) != 1 || reqs[0].rawPath != "/api/v1/apps/connectors/credentials" {
		t.Fatalf("requests %v", reqs)
	}
	assertJSONEqual(t, reqs[0].body, `{"appId":"a1","connectorId":"cn1"}`)
	if secretWording(secretConnectorCredential, nil) == "" {
		t.Error("rotate's secret kind has no wording")
	}
}

func assertJSONEqual(t *testing.T, got, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal([]byte(got), &g); err != nil {
		t.Fatalf("body %q: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("body %s, want %s", got, want)
	}
}
