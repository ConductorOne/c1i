package cmd

import (
	"net/http"
	"strings"
	"testing"
)

// mcp bindings delete no longer echoes the caller's input.
func TestMcpBindingsDeleteDropsSynthesizedOutput(t *testing.T) {
	s := newMutServer(t, fixedReply(200, `{}`))
	t.Setenv("C1I_URL", mutTestURL)
	r := runMutationCLI(t, s.srv, 0, nil, "mcp", "bindings", "delete", "--app-id=a1", "--connector-id=cn1", "--toolset-id=ts1", "--tool-id=t1", "--tool-id=t2")
	if r.stdout != "{}\n" || strings.Contains(r.stdout, "unbound") {
		t.Errorf("stdout = %q, want {}", r.stdout)
	}
	if got := s.requests()[0].body; !strings.Contains(got, `"mcpToolIds":["t1","t2"]`) {
		t.Errorf("body = %s, want both tool ids", got)
	}
}

// Flag errors raised before the helper still carry applied:false.
func TestMcpMutationUsageErrorAppliedFalse(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	r := runMutationCLI(t, nil, 0, nil, "mcp", "servers", "update", "cn1", "--app-id=a1", "--error-format", "json")
	if r.exit != exitUsage {
		t.Fatalf("exit %d, want 2", r.exit)
	}
	if got := r.errorObject(t)["applied"]; got != false {
		t.Errorf("applied = %v, want false", got)
	}
}

// update-credentials resolves a hosted server's catalog id first, reports it
// on stderr only, and does so on a preview too.
func TestMcpUpdateCredentialsResolvesCatalogID(t *testing.T) {
	route := func(r *http.Request, _ int) mutReply {
		if r.Method == http.MethodGet {
			return mutReply{body: `{"mcpServer":{"mcpServerCatalogId":"cat-1"}}`}
		}
		return mutReply{body: `{"mcpServer":{"connectorId":"cn1"}}`}
	}
	args := []string{"mcp", "servers", "update-credentials", "cn1", "--app-id=a1", "--type=hosted", "--auth=bearer-token", "--bearer-token=placeholder", "--error-format=json"}
	for _, dry := range []bool{false, true} {
		name := "send"
		if dry {
			name = "dry run"
		}
		t.Run(name, func(t *testing.T) {
			if dry {
				withDryRun(t)
			}
			s := newMutServer(t, route)
			t.Setenv("C1I_URL", mutTestURL)
			r := runMutationCLI(t, s.srv, 0, nil, args...)
			if r.err != nil {
				t.Fatalf("exit %d: %v\n%s", r.exit, r.err, r.stderr)
			}
			var notice map[string]any
			for _, o := range r.stderrObjects(t) {
				if o["kind"] == "resolved" {
					notice = o
				}
			}
			if notice == nil || notice["name"] != "mcpServerCatalogId" || notice["id"] != "cat-1" {
				t.Errorf("stderr = %q, want a resolved notice for cat-1", r.stderr)
			}
			if strings.Contains(r.stdout, "Resolved") || strings.Contains(r.stdout, "resolved") {
				t.Errorf("stdout = %q, want no resolved line", r.stdout)
			}
			if dry && !strings.Contains(r.stdout, `"mcpServerCatalogId": "cat-1"`) {
				t.Errorf("preview = %s, want the resolved catalog id in the body", r.stdout)
			}
			for _, rq := range s.requests() {
				if dry && rq.method != http.MethodGet {
					t.Errorf("dry run sent %s %s", rq.method, rq.rawPath)
				}
			}
		})
	}
}

// approve prints one compact line per applied id, in argument order, and a
// partial failure reports per-id errors and the first failure's exit code.
func TestMcpToolsApproveNDJSON(t *testing.T) {
	route := func(r *http.Request, _ int) mutReply {
		if strings.HasSuffix(r.URL.Path, "/t2") {
			return mutReply{status: 404, body: `{"code":5}`}
		}
		id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		return mutReply{body: `{"tool":{"id":"` + id + `","state":"MCP_TOOL_STATE_APPROVED"}}`}
	}
	t.Run("all applied", func(t *testing.T) {
		s := newMutServer(t, route)
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, s.srv, 0, nil, "mcp", "tools", "approve", "t1", "t3", "--app-id=a1", "--connector-id=cn1")
		want := `{"id":"t1","state":"MCP_TOOL_STATE_APPROVED"}` + "\n" + `{"id":"t3","state":"MCP_TOOL_STATE_APPROVED"}` + "\n"
		if r.err != nil || r.stdout != want {
			t.Errorf("exit %d stdout %q, want %q", r.exit, r.stdout, want)
		}
	})
	t.Run("one id still prints a compact line", func(t *testing.T) {
		s := newMutServer(t, route)
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, s.srv, 0, nil, "mcp", "tools", "approve", "t1", "--app-id=a1", "--connector-id=cn1")
		if want := `{"id":"t1","state":"MCP_TOOL_STATE_APPROVED"}` + "\n"; r.stdout != want {
			t.Errorf("stdout = %q, want %q", r.stdout, want)
		}
	})
	t.Run("partial failure", func(t *testing.T) {
		s := newMutServer(t, route)
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, s.srv, 0, nil, "mcp", "tools", "approve", "t1", "t2", "t3", "--app-id=a1", "--connector-id=cn1", "--error-format=json")
		want := `{"id":"t1","state":"MCP_TOOL_STATE_APPROVED"}` + "\n" + `{"id":"t3","state":"MCP_TOOL_STATE_APPROVED"}` + "\n"
		if r.exit != exitNotFound || r.stdout != want {
			t.Errorf("exit %d stdout %q, want exit 4 and the two applied lines", r.exit, r.stdout)
		}
		objs := r.stderrObjects(t)
		if len(objs) != 2 || objs[0]["id"] != "t2" || objs[0]["applied"] != false {
			t.Fatalf("stderr = %q, want a per-id error for t2 then a summary", r.stderr)
		}
		if objs[1]["applied"] != true || objs[1]["succeeded"] != float64(2) || objs[1]["failed"] != float64(1) {
			t.Errorf("summary = %v", objs[1])
		}
	})
	t.Run("removed state is refused", func(t *testing.T) {
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, nil, 0, nil, "mcp", "tools", "approve", "t1", "--app-id=a1", "--connector-id=cn1", "--state=removed", "--error-format=json")
		if r.exit != exitUsage || r.stdout != "" || r.errorObject(t)["applied"] != false {
			t.Errorf("exit %d stdout %q stderr %q", r.exit, r.stdout, r.stderr)
		}
	})
}
