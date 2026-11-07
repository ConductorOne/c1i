package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var errStdoutFull = errors.New("no space left on device")

// failingWriter stands in for stdout on a full disk.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errStdoutFull }

// assertMutationWriteFailure is assertOutputWriteFailure for a mutation,
// whose write failure happens after the change was applied.
func assertMutationWriteFailure(t *testing.T, err error, request string) {
	t.Helper()
	if err == nil {
		t.Fatal("RunE returned nil although every write to stdout failed")
	}
	if !errors.Is(err, errStdoutFull) || exitCode(err) != exitError {
		t.Errorf("error %v (exit %d) does not wrap the write error with exit 1", err, exitCode(err))
	}
	if want := request + " succeeded but writing its response failed: "; !strings.HasPrefix(err.Error(), want) {
		t.Errorf("error %q, want prefix %q", err, want)
	}
}

func assertOutputWriteFailure(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Error("RunE returned nil although every write to stdout failed")
		return
	}
	if !errors.Is(err, errStdoutFull) {
		t.Errorf("error %v does not wrap the write error", err)
	}
	if got := exitCode(err); got != exitError {
		t.Errorf("exit code %d, want %d", got, exitError)
	}
	if msg := err.Error(); strings.Count(msg, "writing output: ") != 1 || strings.Contains(msg, "write output") {
		t.Errorf("error %q should say \"writing output: \" exactly once", msg)
	}
}

// Every auto-paginating list (typed rows, listJSONPages-backed, export events)
// must fail on the first failed write and not fetch another page.
func TestListCommandsStopOnOutputWriteError(t *testing.T) {
	for _, tc := range listPaginationCases() {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				n := requests.Add(1)
				next := ""
				if n < 3 {
					next = fmt.Sprintf("tok%d", n)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, tc.page([]string{fmt.Sprintf("item%d", n)}, next))
			}))
			defer srv.Close()
			t.Cleanup(func() { tc.cmd.SetOut(nil) })

			_, err := runListPaginationCaseTo(t, tc, srv, failingWriter{})
			assertOutputWriteFailure(t, err)
			if got := requests.Load(); got != 1 {
				t.Errorf("server received %d requests, want 1: paging continued after a failed write", got)
			}
		})
	}
}

// Single-object reads go through writeObject; cover both its projected and
// raw branches.
func TestGetCommandsFailOnOutputWriteError(t *testing.T) {
	for _, fields := range []string{"", "id"} {
		for _, c := range getUnwrapCases() {
			t.Run(fmt.Sprintf("%s/fields=%q", c.name, fields), func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprint(w, c.body)
				}))
				defer srv.Close()

				resetCmdFlags(t, c.cmd)
				stubGetClient(t, srv)
				t.Setenv("C1I_URL", "https://example.invalid")
				viper.Set("fields", fields)
				t.Cleanup(func() { viper.Set("fields", "") })
				for name, val := range c.flags {
					if err := c.cmd.Flags().Set(name, val); err != nil {
						t.Fatalf("set --%s: %v", name, err)
					}
				}
				c.cmd.SetOut(failingWriter{})
				t.Cleanup(func() { c.cmd.SetOut(nil) })
				c.cmd.SetContext(context.Background())

				err := c.cmd.RunE(c.cmd, c.args)
				var ue *usageError
				if fields != "" && errors.As(err, &ue) {
					t.Skipf("--fields %s does not apply to this response", fields)
				}
				assertOutputWriteFailure(t, err)
			})
		}
	}
}

// Text confirmations, dry-run previews and plain-text reads must not exit 0
// when stdout is gone; for a batch, the next id is not sent.
func TestTextOutputFailsOnOutputWriteError(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		reset    []*cobra.Command
		requests int32
		mutation string // the applied request a failed write reports
		summary  string // a batch abort's message prefix instead (exit 1; the batch stops)
	}{
		{name: "version", args: []string{"version"}},
		{name: "docs guide list", args: []string{"docs", "guide"}},
		{name: "apps delete", args: []string{"apps", "delete", "app-x"}, reset: []*cobra.Command{appsDeleteCmd}, requests: 1, mutation: "DELETE /api/v1/apps/app-x"},
		{name: "automations delete", args: []string{"automations", "delete", "auto-x"}, reset: []*cobra.Command{automationsDeleteCmd}, requests: 1},
		{
			name:     "mcp tools delete",
			args:     []string{"mcp", "tools", "delete", "tool-a", "--app-id", "app-x", "--connector-id", "conn-y"},
			reset:    []*cobra.Command{mcpToolsDeleteCmd},
			requests: 1,
			mutation: "DELETE /api/v1/apps/app-x/connectors/conn-y/mcp_tools/tool-a",
		},
		{
			name:     "mcp tools approve batch",
			args:     []string{"mcp", "tools", "approve", "tool-a", "tool-b", "--app-id", "app-x", "--connector-id", "conn-y"},
			reset:    []*cobra.Command{mcpToolsApproveCmd},
			requests: 1,
			summary:  "approved 0 of 2 tools; 1 failed; output failed: POST /api/v1/apps/app-x/connectors/conn-y/mcp_tools/tool-a succeeded but writing its response failed: ",
		},
		{name: "apps create dry-run", args: []string{"apps", "create", "--display-name", "zz-x", "--dry-run"}, reset: []*cobra.Command{appsCreateCmd}},
		{name: "api paginate", args: []string{"api", "--path", "/api/v1/things", "--paginate"}, reset: []*cobra.Command{apiCmd}, requests: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `{"tool":{"id":"tool-a","state":"MCP_TOOL_STATE_APPROVED"},"list":[{"id":"r1"}],"nextPageToken":"t1"}`)
			}))
			defer srv.Close()
			stubGetClient(t, srv)
			resetCmdFlags(t, rootCmd)
			resetCmds(t, tc.reset...)
			t.Cleanup(func() { resetCmds(t, tc.reset...) })
			if slices.Contains(tc.args, "--dry-run") {
				withDryRun(t)
			}
			t.Setenv("C1I_URL", "https://example.invalid")

			var errOut bytes.Buffer
			rootCmd.SetOut(failingWriter{})
			rootCmd.SetErr(&errOut)
			t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil) })
			// A leaf's own writer or context (left by another test) shadows root's.
			leaf, _, findErr := rootCmd.Find(tc.args)
			if findErr != nil {
				t.Fatal(findErr)
			}
			leaf.SetOut(failingWriter{})
			leaf.SetContext(context.Background())
			t.Cleanup(func() { leaf.SetOut(nil) })
			rootCmd.SetArgs(tc.args)
			err := rootCmd.ExecuteContext(context.Background())
			switch {
			case tc.summary != "":
				if err == nil || exitCode(err) != exitError || !strings.HasPrefix(err.Error(), tc.summary) {
					t.Errorf("error %v (exit %d), want exit 1 and prefix %q", err, exitCode(err), tc.summary)
				}
			case tc.mutation != "":
				assertMutationWriteFailure(t, err, tc.mutation)
			default:
				assertOutputWriteFailure(t, err)
			}
			if got := requests.Load(); got != tc.requests {
				t.Errorf("server received %d requests, want %d", got, tc.requests)
			}
		})
	}
}

// runUpgradeToDeadStdout runs `upgrade` against the fake dist with a stdout
// that always fails.
func runUpgradeToDeadStdout(t *testing.T, args ...string) error {
	t.Helper()
	resetCmds(t, upgradeCmd)
	resetCmdFlags(t, rootCmd)
	var errOut bytes.Buffer
	rootCmd.SetOut(failingWriter{})
	rootCmd.SetErr(&errOut)
	upgradeCmd.SetOut(failingWriter{})
	t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil); upgradeCmd.SetOut(nil) })
	upgradeCmd.SetContext(context.Background())
	rootCmd.SetArgs(append([]string{"upgrade"}, args...))
	return rootCmd.ExecuteContext(context.Background())
}

// `upgrade --yes` is scriptable, so its result lines must not exit 0 unseen.
func TestUpgradeResultFailsOnOutputWriteError(t *testing.T) {
	t.Run("already current", func(t *testing.T) {
		d := newFakeDist(t)
		publishIndex(t, d, idxStable06Latest07)
		useDist(t, d, nil, "v0.7.0")
		assertOutputWriteFailure(t, runUpgradeToDeadStdout(t, "--channel", "latest", "-y"))
	})
	t.Run("not standalone", func(t *testing.T) {
		d := newFakeDist(t)
		publishIndex(t, d, idxStable06Latest07)
		useDist(t, d, nil, "v0.5.0")
		upgradeExecutable = func() (string, error) { return "/opt/homebrew/Cellar/c1i/0.5.0/bin/c1i", nil }
		assertOutputWriteFailure(t, runUpgradeToDeadStdout(t, "-y"))
	})
	t.Run("upgraded", func(t *testing.T) {
		sig, d := newFakeSigstore(t), newFakeDist(t)
		d.publish(t, sig, release{target: "v0.7.0", manifestSemver: "v0.7.0", archive: tarGz(t, []byte("NEW BINARY"))})
		exe := useDist(t, d, sig.root, "v0.6.0")
		assertOutputWriteFailure(t, runUpgradeToDeadStdout(t, "-y"))
		if got := readFile(t, exe); got != "NEW BINARY" {
			t.Errorf("binary = %q; the upgrade itself should still land", got)
		}
	})
}

// A batch approve that loses stdout mid-way aborts: no further id is sent, the
// summary names the failed write, and the exit is 1 even though an earlier id
// 404ed (the abort override, R33).
func TestMcpToolsApproveOutputFailureKeepsSummary(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if strings.HasSuffix(r.URL.Path, "/tool-a") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"message":"not found"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"tool":{"id":"tool-b","state":"MCP_TOOL_STATE_APPROVED"}}`)
	}))
	defer srv.Close()
	stubGetClient(t, srv)
	resetCmdFlags(t, rootCmd)
	resetCmds(t, mcpToolsApproveCmd)
	t.Cleanup(func() { resetCmds(t, mcpToolsApproveCmd) })
	t.Setenv("C1I_URL", "https://example.invalid")

	var errOut bytes.Buffer
	rootCmd.SetOut(failingWriter{})
	rootCmd.SetErr(&errOut)
	mcpToolsApproveCmd.SetOut(failingWriter{})
	mcpToolsApproveCmd.SetContext(context.Background())
	t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil); mcpToolsApproveCmd.SetOut(nil) })
	rootCmd.SetArgs([]string{"mcp", "tools", "approve", "tool-a", "tool-b", "tool-c", "--app-id", "app-x", "--connector-id", "conn-y"})
	err := rootCmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("approve returned nil although stdout failed")
	}
	want := "approved 0 of 3 tools; 2 failed; output failed: POST /api/v1/apps/app-x/connectors/conn-y/mcp_tools/tool-b succeeded but writing its response failed: "
	if !strings.HasPrefix(err.Error(), want) {
		t.Errorf("error %q, want prefix %q", err, want)
	}
	if got := exitCode(err); got != exitError {
		t.Errorf("exit code %d, want %d (the batch aborted on a write failure)", got, exitError)
	}
	if got := requests.Load(); got != 2 {
		t.Errorf("server received %d requests, want 2 (tool-c must not be sent)", got)
	}
}

// A mutation that fails only at output may have landed; agents must know not
// to blindly retry it.
func TestOutputWriteContractDocumented(t *testing.T) {
	readme, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	for name, doc := range map[string]string{"README.md": string(readme), "cmd/agents.md": agentsTemplate} {
		if !strings.Contains(doc, "`writing output:`") || !strings.Contains(doc, "verify before retrying a mutation") {
			t.Errorf("%s does not document that exit 1 with `writing output:` may follow a successful mutation", name)
		}
	}
	if err := wrapOutputErr(errStdoutFull); !strings.HasPrefix(err.Error(), "writing output: ") {
		t.Errorf("wrapOutputErr message %q no longer matches the documented prefix", err)
	}
}

func TestWriteRawObjectFailsOnOutputWriteError(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetOut(failingWriter{})
	assertOutputWriteFailure(t, writeRawObject(cmd, []byte(`{"deleted":true}`)))
}

// --limit compares against Written, so a failed row must not count.
func TestEmitterCountsOnlyWrittenRows(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetOut(failingWriter{})
	cmd.SetContext(context.Background())
	enc := newEmitter(cmd)
	if err := enc.Encode(map[string]string{"id": "x"}); !errors.Is(err, errStdoutFull) {
		t.Fatalf("Encode error = %v, want it to wrap the write error", err)
	}
	if got := enc.Written(); got != 0 {
		t.Errorf("Written() = %d after a failed write, want 0", got)
	}

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	enc = newEmitter(cmd)
	if err := enc.Encode(map[string]string{"id": "x"}); err != nil {
		t.Fatal(err)
	}
	if got := enc.Written(); got != 1 {
		t.Errorf("Written() = %d after a successful write, want 1", got)
	}
}

// stdoutPromptExempt lists interactive flows (whole files, or file:func) whose
// stdout is prompts and progress for a person at a terminal, not output a
// caller parses; they may discard a write error.
var stdoutPromptExempt = map[string]bool{
	"auth_login.go":       true,
	"auth_login_roles.go": true,
	"upgrade.go:confirm":  true,
}

// writerParamExempt lists functions (file:func, or file:(T).method) whose
// io.Writer parameter may discard a write error, with the reason; every other
// io.Writer parameter is scanned as stdout. Each entry trusts its callers.
var writerParamExempt = map[string]string{
	"errors.go:writeError":             "the final error, written to stderr with nowhere left to report a failure; trusts its one caller to pass os.Stderr",
	"functions_source.go:hardenOutDir": "a permissions notice on stderr; trusts its one caller to pass cmd.ErrOrStderr()",
	"stderr.go:noticeTo":               "a stderr notice; its callers pass stderr (TestStderrWritesOnlyThroughHelpers)",
	"stderr.go:progress":               "a --wait progress line; runWait passes stderr",
	"stderr.go:writeJSONLine":          "one JSON-mode stderr line; only stderr.go and errors.go call it",
	"upgrade.go:upgradeProgress":       "progress on stdout; trusts every caller to follow it with a checked result write",
}

// TestNoDiscardedOutputWriteErrors fails on any `_ = x.Encode(...)`,
// `_, _ = x.Write(...)`, bare `x.Encode(...)`, or discarded `fmt.Fprint*` to
// stdout in cmd/: a discarded write error turns a truncated output into
// exit 0. Stdout means cmd.OutOrStdout(), os.Stdout, a local assigned from
// either, or an io.Writer parameter of an enclosing func outside
// writerParamExempt: checking the callee, not call sites, needs no
// cross-function tracing. Stderr notices may discard their error.
func TestNoDiscardedOutputWriteErrors(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil || len(paths) == 0 {
		t.Fatalf("globbing cmd/*.go: %v (found %d)", err, len(paths))
	}
	for key := range stdoutPromptExempt {
		file, _, _ := strings.Cut(key, ":")
		if !slices.Contains(paths, file) {
			t.Errorf("stdoutPromptExempt names %s, which no longer exists", key)
		}
	}
	isWriterType := func(e ast.Expr) bool {
		if el, ok := e.(*ast.Ellipsis); ok {
			e = el.Elt
		}
		sel, ok := e.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Writer" {
			return false
		}
		pkg, ok := sel.X.(*ast.Ident)
		return ok && pkg.Name == "io"
	}
	// writerParams returns the names of ft's own io.Writer parameters, and
	// whether it has any (named or not). A func-typed parameter's inner
	// signature is not a parameter list of ft.
	writerParams := func(ft *ast.FuncType) (names []string, found bool) {
		for _, field := range ft.Params.List {
			if !isWriterType(field.Type) {
				continue
			}
			found = true
			for _, n := range field.Names {
				names = append(names, n.Name)
			}
		}
		return names, found
	}
	writerParamsSeen := map[string]bool{}
	isWriteCall := func(e ast.Expr) (string, bool) {
		call, ok := e.(*ast.CallExpr)
		if !ok {
			return "", false
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "Encode" && sel.Sel.Name != "Write") {
			return "", false
		}
		return sel.Sel.Name, true
	}
	isStdoutExpr := func(e ast.Expr) bool {
		switch x := e.(type) {
		case *ast.CallExpr:
			sel, ok := x.Fun.(*ast.SelectorExpr)
			return ok && sel.Sel.Name == "OutOrStdout"
		case *ast.SelectorExpr:
			pkg, ok := x.X.(*ast.Ident)
			return ok && pkg.Name == "os" && x.Sel.Name == "Stdout"
		}
		return false
	}
	// stdoutFprint matches a fmt.Fprint* whose writer is stdout, given which
	// names hold stdout at that point.
	stdoutFprint := func(e ast.Expr, isStdoutName func(string) bool) (string, bool) {
		call, ok := e.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return "", false
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !strings.HasPrefix(sel.Sel.Name, "Fprint") {
			return "", false
		}
		if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "fmt" {
			return "", false
		}
		if isStdoutExpr(call.Args[0]) {
			return "fmt." + sel.Sel.Name, true
		}
		w := call.Args[0]
		if ix, ok := w.(*ast.IndexExpr); ok { // ws[i] of a ...io.Writer param
			w = ix.X
		}
		if id, ok := w.(*ast.Ident); ok && isStdoutName(id.Name) {
			return "fmt." + sel.Sel.Name, true
		}
		return "", false
	}
	allBlank := func(lhs []ast.Expr) bool {
		for _, l := range lhs {
			if id, ok := l.(*ast.Ident); !ok || id.Name != "_" {
				return false
			}
		}
		return true
	}
	var scanned, stdoutFprints int
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		scanned++
		for _, decl := range file.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)
			declKey := path
			if isFunc {
				declKey = path + ":" + fn.Name.Name
				if fn.Recv != nil && len(fn.Recv.List) > 0 {
					declKey = fmt.Sprintf("%s:(%s).%s", path, types.ExprString(fn.Recv.List[0].Type), fn.Name.Name)
				}
			}
			promptOnly := stdoutPromptExempt[path] || (isFunc && stdoutPromptExempt[declKey])
			_, paramsExempt := writerParamExempt[declKey]
			// Locals are collected per top-level decl, so a name reused for
			// stderr elsewhere in the same decl over-flags (loudly) rather
			// than under-flags.
			locals := map[string]bool{}
			ast.Inspect(decl, func(n ast.Node) bool {
				if as, ok := n.(*ast.AssignStmt); ok && len(as.Lhs) == len(as.Rhs) {
					for i, r := range as.Rhs {
						if id, ok := as.Lhs[i].(*ast.Ident); ok && isStdoutExpr(r) {
							locals[id.Name] = true
						}
					}
				}
				return true
			})
			// scopes holds, per node on the current path, the io.Writer
			// params its signature declares (nil for a non-func node), so a
			// param is stdout only inside the func that declares it.
			var scopes [][]string
			isStdoutName := func(name string) bool {
				if locals[name] {
					return true
				}
				for _, sc := range scopes {
					if slices.Contains(sc, name) {
						return true
					}
				}
				return false
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				if n == nil {
					scopes = scopes[:len(scopes)-1]
					return true
				}
				var ft *ast.FuncType
				switch f := n.(type) {
				case *ast.FuncDecl:
					ft = f.Type
				case *ast.FuncLit:
					ft = f.Type
				}
				var params []string
				if ft != nil {
					names, found := writerParams(ft)
					if found {
						writerParamsSeen[declKey] = true
					}
					if !paramsExempt {
						params = names
					}
				}
				// `for _, w := range ws` over a ...io.Writer param.
				if rs, ok := n.(*ast.RangeStmt); ok {
					x, xok := rs.X.(*ast.Ident)
					v, vok := rs.Value.(*ast.Ident)
					if xok && vok && isStdoutName(x.Name) {
						params = []string{v.Name}
					}
				}
				scopes = append(scopes, params)
				switch s := n.(type) {
				case *ast.CallExpr:
					if _, ok := stdoutFprint(s, isStdoutName); ok {
						stdoutFprints++
					}
				case *ast.AssignStmt:
					if len(s.Rhs) != 1 || !allBlank(s.Lhs) {
						return true
					}
					if name, ok := isWriteCall(s.Rhs[0]); ok {
						t.Errorf("%s: discards the error from %s; return it so a failed write can't exit 0", fset.Position(s.Pos()), name)
					}
					if name, ok := stdoutFprint(s.Rhs[0], isStdoutName); ok && !promptOnly {
						t.Errorf("%s: discards the error from %s to stdout; return wrapOutputErr(err) so a failed write can't exit 0", fset.Position(s.Pos()), name)
					}
				case *ast.ExprStmt:
					// Bare Write is left to errcheck, which knows a bytes.Buffer can't fail.
					if name, ok := isWriteCall(s.X); ok && name == "Encode" {
						t.Errorf("%s: ignores the error from %s; return it so a failed write can't exit 0", fset.Position(s.Pos()), name)
					}
					if name, ok := stdoutFprint(s.X, isStdoutName); ok && !promptOnly {
						t.Errorf("%s: ignores the error from %s to stdout; return wrapOutputErr(err) so a failed write can't exit 0", fset.Position(s.Pos()), name)
					}
				}
				return true
			})
		}
	}
	if scanned == 0 {
		t.Fatal("scanned no non-test files in cmd/")
	}
	for key := range writerParamExempt {
		if !writerParamsSeen[key] {
			t.Errorf("writerParamExempt names %s, which no longer has an io.Writer parameter", key)
		}
	}
	if stdoutFprints == 0 {
		t.Fatal("recognized no fmt.Fprint* to stdout anywhere; the stdout matcher is broken")
	}
}
