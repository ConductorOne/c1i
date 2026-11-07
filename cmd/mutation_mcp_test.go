package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// mcpMutationCase is one mcp mutation, its canned response and the compact
// document it must print.
type mcpMutationCase struct {
	name string
	args func(t *testing.T) []string
	body string // the server's response
	want string // compact stdout
}

func mcpMutationCases() []mcpMutationCase {
	const scope = "--app-id=a1"
	static := func(args ...string) func(*testing.T) []string {
		return func(*testing.T) []string { return args }
	}
	withFile := func(content string, before []string, after ...string) func(*testing.T) []string {
		return func(t *testing.T) []string {
			file := writeClassifierJSON(t, "body.json", content)
			return append(append(append([]string{}, before...), "--body-file="+file), after...)
		}
	}
	const classifier = `{"classifier":{"id":"cl1","displayName":"zz"}}`
	const classifierDoc = `{"id":"cl1","displayName":"zz"}`
	const gate = `{"toolGate":{"id":"g1","displayName":"zz"}}`
	const gateDoc = `{"id":"g1","displayName":"zz"}`
	return []mcpMutationCase{
		{"bindings create", static("mcp", "bindings", "create", scope, "--connector-id=cn1", "--toolset-id=ts1", "--tool-id=t1"),
			`{"bindings":[{"mcpToolId":"t1"}]}`, `{"bindings":[{"mcpToolId":"t1"}]}`},
		{"bindings delete", static("mcp", "bindings", "delete", scope, "--connector-id=cn1", "--toolset-id=ts1", "--tool-id=t1"), `{}`, `{}`},
		{"classifiers create", withFile(`{"displayName":"zz"}`, []string{"mcp", "classifiers", "create"}), classifier, classifierDoc},
		{"classifiers update", withFile(`{"displayName":"zz"}`, []string{"mcp", "classifiers", "update", "cl1"}, "--update-mask=description"), classifier, classifierDoc},
		{"classifiers delete", static("mcp", "classifiers", "delete", "cl1"), `{}`, `{}`},
		{"templates add-rule", withFile(`{}`, []string{"mcp", "classifiers", "templates", "add-rule", "cl1", "tp1"}), classifier, classifierDoc},
		{"templates instantiate", static("mcp", "classifiers", "templates", "instantiate", "tp1"), classifier, classifierDoc},
		{"classifier bindings create", withFile(`{"surface":"S","classifierId":"cl1"}`, []string{"mcp", "classifiers", "bindings", "create"}),
			`{"binding":{"id":"b1","surface":"S"}}`, `{"id":"b1","surface":"S"}`},
		{"classifier bindings delete", static("mcp", "classifiers", "bindings", "delete", "b1"), `{}`, `{}`},
		{"policy update", withFile(`{"defaultOutcome":"X"}`, []string{"mcp", "classifiers", "policy", "update"}, "--update-mask=defaultOutcome"),
			`{"policy":{"defaultOutcome":"X"}}`, `{"defaultOutcome":"X"}`},
		{"tool-gates create", withFile(`{"displayName":"zz"}`, []string{"mcp", "classifiers", "tool-gates", "create"}), gate, gateDoc},
		{"tool-gates update", withFile(`{"displayName":"zz"}`, []string{"mcp", "classifiers", "tool-gates", "update", "g1"}, "--update-mask=enabled"), gate, gateDoc},
		{"tool-gates delete", static("mcp", "classifiers", "tool-gates", "delete", "g1"), `{}`, `{}`},
		{"servers register", static("mcp", "servers", "register", scope, "--type=external", "--display-name=zz", "--server-url=https://mcp.example.invalid"),
			`{"mcpServer":{"connectorId":"cn1","displayName":"zz"},"accessProfilesAttached":2}`,
			`{"connectorId":"cn1","displayName":"zz","accessProfilesAttached":2}`},
		{"servers update", static("mcp", "servers", "update", "cn1", scope, "--display-name=zz"),
			`{"mcpServer":{"connectorId":"cn1","displayName":"zz"}}`, `{"connectorId":"cn1","displayName":"zz"}`},
		{"servers update-credentials", static("mcp", "servers", "update-credentials", "cn1", scope, "--type=external", "--server-url=https://mcp.example.invalid"),
			`{"mcpServer":{"connectorId":"cn1"}}`, `{"connectorId":"cn1"}`},
		{"servers delete", static("mcp", "servers", "delete", "cn1", scope), `{}`, `{}`},
		{"servers resync-tools", static("mcp", "servers", "resync-tools", "cn1", scope), `{}`, `{}`},
		{"tools approve", static("mcp", "tools", "approve", "t1", scope, "--connector-id=cn1"),
			`{"tool":{"id":"t1","state":"MCP_TOOL_STATE_APPROVED"}}`, `{"id":"t1","state":"MCP_TOOL_STATE_APPROVED"}`},
		{"tools delete", static("mcp", "tools", "delete", "t1", scope, "--connector-id=cn1"), `{}`, `{}`},
		{"toolsets create", static("mcp", "toolsets", "create", scope, "--connector-id=cn1", "--display-name=zz"),
			`{"profile":{"id":"ts1","displayName":"zz"}}`, `{"id":"ts1","displayName":"zz"}`},
		{"toolsets update", static("mcp", "toolsets", "update", "ts1", scope, "--connector-id=cn1", "--display-name=zz"),
			`{"profile":{"id":"ts1","displayName":"zz"}}`, `{"id":"ts1","displayName":"zz"}`},
		{"toolsets delete", static("mcp", "toolsets", "delete", "ts1", scope, "--connector-id=cn1"), `{}`, `{}`},
	}
}

func indentJSON(t *testing.T, compact string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(compact), "", "  "); err != nil {
		t.Fatal(err)
	}
	return buf.String() + "\n"
}

// Each command prints its family's read shape, byte for byte, and nothing
// else on either stream.
func TestMcpMutationStdout(t *testing.T) {
	for _, tc := range mcpMutationCases() {
		t.Run(tc.name, func(t *testing.T) {
			s := newMutServer(t, fixedReply(200, tc.body))
			t.Setenv("C1I_URL", mutTestURL)
			r := runMutationCLI(t, s.srv, 0, nil, tc.args(t)...)
			if r.err != nil {
				t.Fatalf("exit %d: %v\n%s", r.exit, r.err, r.stderr)
			}
			want := indentJSON(t, tc.want)
			if tc.name == "tools approve" { // a batch line is compact
				want = tc.want + "\n"
			}
			if r.stdout != want {
				t.Errorf("stdout = %q, want %q", r.stdout, want)
			}
			if r.stderr != "" {
				t.Errorf("stderr = %q, want nothing", r.stderr)
			}
			if len(s.requests()) != 1 {
				t.Errorf("%d requests, want 1", len(s.requests()))
			}
		})
	}
	t.Run("empty body prints {}", func(t *testing.T) {
		s := newMutServer(t, fixedReply(200, ``))
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, s.srv, 0, nil, "mcp", "servers", "delete", "cn1", "--app-id=a1")
		if r.err != nil || r.stdout != "{}\n" {
			t.Errorf("exit %d stdout %q, want {}", r.exit, r.stdout)
		}
	})
}

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

// A preview is the helper's document: one request, no id, a stderr notice.
func TestMcpMutationPreview(t *testing.T) {
	for _, tc := range mcpMutationCases() {
		t.Run(tc.name, func(t *testing.T) {
			withDryRun(t)
			t.Setenv("C1I_URL", mutTestURL)
			r := runMutationCLI(t, nil, 0, nil, tc.args(t)...)
			if r.err != nil {
				t.Fatalf("exit %d: %v", r.exit, r.err)
			}
			var doc struct {
				DryRun   bool `json:"dryRun"`
				Requests []struct {
					Method string `json:"method"`
					URL    string `json:"url"`
				} `json:"requests"`
			}
			if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil || !doc.DryRun || len(doc.Requests) == 0 {
				t.Fatalf("stdout = %q, want a preview document", r.stdout)
			}
			for _, req := range doc.Requests {
				if !strings.HasPrefix(req.URL, mutTestURL+"/api/v1/") {
					t.Errorf("url = %q, want the tenant URL and path", req.URL)
				}
			}
			if r.stderr != "dry run: nothing was sent\n" {
				t.Errorf("stderr = %q, want the dry-run notice", r.stderr)
			}
		})
	}
}

// A mutation refuses --fields before anything is sent, even on a preview.
func TestMcpMutationRefusesFields(t *testing.T) {
	for _, tc := range mcpMutationCases() {
		t.Run(tc.name, func(t *testing.T) {
			s := newMutServer(t, fixedReply(200, tc.body))
			t.Setenv("C1I_URL", mutTestURL)
			r := runMutationCLI(t, s.srv, 0, nil, append(tc.args(t), "--fields", "id", "--error-format", "json")...)
			if r.exit != exitUsage || r.stdout != "" || len(s.requests()) != 0 {
				t.Errorf("exit %d stdout %q, %d requests; want exit 2 and nothing sent", r.exit, r.stdout, len(s.requests()))
			}
			if got := r.errorObject(t)["applied"]; got != false {
				t.Errorf("applied = %v, want false", got)
			}
		})
	}
}

// A 404 reports applied:false with the not-found exit code.
func TestMcpMutationAppliedOn404(t *testing.T) {
	for _, tc := range mcpMutationCases() {
		t.Run(tc.name, func(t *testing.T) {
			s := newMutServer(t, fixedReply(404, `{"code":5}`))
			t.Setenv("C1I_URL", mutTestURL)
			r := runMutationCLI(t, s.srv, 0, nil, append(tc.args(t), "--error-format", "json")...)
			if r.exit != exitNotFound || r.stdout != "" {
				t.Errorf("exit %d stdout %q, want exit 4 and nothing on stdout", r.exit, r.stdout)
			}
			if got := r.errorObject(t)["applied"]; got != false {
				t.Errorf("applied = %v, want false", got)
			}
		})
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
