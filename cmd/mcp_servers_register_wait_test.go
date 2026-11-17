package cmd

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func registerWaitArgs(extra ...string) []string {
	return append([]string{"mcp", "servers", "register", "--app-id", "app-1", "--type", "external",
		"--display-name", "zz-wait", "--server-url", "https://example.invalid/mcp", "--auth", "none"}, extra...)
}

func TestMCPServersRegisterWait(t *testing.T) {
	orig := mcpServerWaitInterval
	mcpServerWaitInterval = 5 * time.Millisecond
	t.Cleanup(func() { mcpServerWaitInterval = orig })
	const created = `{"mcpServer":{"connectorId":"conn-9","appId":"app-1"}}`

	t.Run("polls the list until the server appears", func(t *testing.T) {
		lists := 0
		s := newMutServer(t, func(r *http.Request, _ int) mutReply {
			if r.Method == http.MethodPost {
				return mutReply{body: created}
			}
			lists++
			if lists < 3 {
				return mutReply{body: `{"list":[{"connectorId":"other"}]}`}
			}
			return mutReply{body: `{"list":[{"connectorId":"other"},{"connectorId":"conn-9"}]}`}
		})
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, s.srv, 0, nil, registerWaitArgs("--wait")...)
		if r.err != nil || lists != 3 || !strings.Contains(r.stdout, `"connectorId": "conn-9"`) {
			t.Errorf("err %v lists %d stdout %q", r.err, lists, r.stdout)
		}
		if !strings.Contains(r.stderr, "conn-9 is listed") {
			t.Errorf("stderr = %q, want the success line", r.stderr)
		}
	})
	t.Run("follows list pages", func(t *testing.T) {
		var page2 atomic.Bool
		s := newMutServer(t, func(r *http.Request, _ int) mutReply {
			if r.Method == http.MethodPost {
				return mutReply{body: created}
			}
			if r.URL.Query().Get("page_token") == "p2" {
				page2.Store(true)
				return mutReply{body: `{"list":[{"connectorId":"conn-9"}]}`}
			}
			return mutReply{body: `{"list":[{"connectorId":"other"}],"nextPageToken":"p2"}`}
		})
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, s.srv, 0, nil, registerWaitArgs("--wait")...)
		if r.err != nil {
			t.Errorf("err %v, want success once page 2 lists it", r.err)
		}
		if !page2.Load() {
			t.Error("page 2 was never requested; the wait did not follow the cursor")
		}
	})
	t.Run("timeout is applied:true and keeps the document", func(t *testing.T) {
		s := newMutServer(t, func(r *http.Request, _ int) mutReply {
			if r.Method == http.MethodPost {
				return mutReply{body: created}
			}
			return mutReply{body: `{"list":[]}`}
		})
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, s.srv, 0, nil, registerWaitArgs("--wait", "--wait-timeout", "50ms", "--error-format", "json")...)
		if r.exit != exitError || !strings.Contains(r.stdout, "conn-9") {
			t.Errorf("exit %d stdout %q, want 1 and the document", r.exit, r.stdout)
		}
		obj := r.errorObject(t)
		if obj["applied"] != true || !strings.Contains(obj["error"].(string), "was applied; waiting for MCP server conn-9") {
			t.Errorf("error object = %v", obj)
		}
	})
	t.Run("response without connectorId exits 6, applied, and polls nothing", func(t *testing.T) {
		s := newMutServer(t, fixedReply(200, `{"mcpServer":{"appId":"app-1"}}`))
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, s.srv, 0, nil, registerWaitArgs("--wait", "--error-format", "json")...)
		if r.exit != exitServer || r.errorObject(t)["applied"] != true {
			t.Errorf("exit %d stderr %s, want 6 and applied:true", r.exit, r.stderr)
		}
		if got := s.requests(); len(got) != 1 {
			t.Errorf("requests = %+v, want only the POST", got)
		}
	})
	t.Run("dry-run sends and polls nothing", func(t *testing.T) {
		withDryRun(t)
		s := newMutServer(t, fixedReply(200, created))
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, s.srv, 0, nil, registerWaitArgs("--wait")...)
		if r.err != nil || len(s.requests()) != 0 {
			t.Errorf("err %v requests %+v, want a preview and no requests", r.err, s.requests())
		}
	})
	t.Run("wait-timeout without wait is a usage error", func(t *testing.T) {
		s := newMutServer(t, fixedReply(200, created))
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, s.srv, 0, nil, registerWaitArgs("--wait-timeout", "5s")...)
		if r.exit != exitUsage || len(s.requests()) != 0 {
			t.Errorf("exit %d, want 2 with nothing sent", r.exit)
		}
	})
}
