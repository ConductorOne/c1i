package cmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// These tests enforce cmd/mutation.go as the single chokepoint for mutation
// output (TestMutationsWriteOnlyThroughHelper and friends). They report every
// command still to migrate; pendingMigration is that checklist, and it can
// only shrink: an entry whose command no longer sends a raw write fails.

// mutatingSelectors are the client methods that can send a write. A match
// counts as a call or a method value.
var mutatingSelectors = map[string]bool{"Post": true, "Put": true, "Patch": true, "Delete": true, "Request": true, "Do": true}

var mutationHelpers = map[string]bool{"sendMutation": true, "sendMutationBatch": true, "sendMutationSequence": true}

// chokepointFiles may hold mutating selectors: the helper, and the transport
// layers and protocol clients beneath or beside it.
var chokepointFiles = []string{"cmd/mutation.go", "internal/client/", "internal/transport/", "internal/mcpgateway/", "internal/login/"}

// readOnlyPOST lists declarations (file:decl) whose POSTs only read: searches
// and lists that carry their filter in the body.
var readOnlyPOST = map[string]string{
	"cmd/accounts_list.go:accountsListCmd":                            "POST /search/app_users",
	"cmd/automations_executions_list.go:automationsExecutionsListCmd": "POST /search/automation_executions",
	"cmd/automations_list.go:automationsListCmd":                      "POST /search/automations",
	"cmd/entitlements_list.go:entitlementsListCmd":                    "POST /search/entitlements",
	"cmd/export_events.go:exportEventsCmd":                            "POST /systemlog/events",
	"cmd/functions_usage.go:functionsUsageCmd":                        "POST /search/automations",
	"cmd/grants_list.go:grantsListCmd":                                "POST /search/grants",
	"cmd/grants_list.go:fetchAllGrants":                               "POST /search/grants, polled by --wait",
	"cmd/mcp_bindings_by_tools.go:mcpBindingsByToolsCmd":              "POST bindings by-tools lookup",
	"cmd/mcp_classifiers.go:mcpClassifierTemplatesListCmd":            "POST /classifier_templates/list",
	"cmd/mcp_classifiers.go:mcpClassifierToolGatesSearchCmd":          "POST /search/tool_gates",
	"cmd/mcp_servers_discover_oidc.go:mcpServersDiscoverOIDCCmd":      "POST discover-oidc probe",
	"cmd/mcp_servers_search.go:mcpServersSearchCmd":                   "POST /search/mcp_servers",
	"cmd/mcp_servers_test_connection.go:mcpServersTestConnectionCmd":  "POST test-connection probe",
	"cmd/mcp_tools_search.go:mcpToolsSearchCmd":                       "POST /search/mcp_tools",
	"cmd/policies_search.go:policiesSearchCmd":                        "POST /search/policies",
	"cmd/policies_validate_cel.go:policiesValidateCelCmd":             "POST validate-cel",
	"cmd/requests_list.go:requestsListCmd":                            "POST /search/tasks",
	"cmd/role_mining.go:postRoleMiningList":                           "POST role-mining list",
	"cmd/service_principals_bindings.go:spBindingsListCmd":            "POST bindings list",
	"cmd/tasks_list.go:tasksListCmd":                                  "POST /search/tasks",
	"cmd/users_list.go:usersListCmd":                                  "POST /search/users",
	"cmd/auth_status.go:authStatusCmd":                                "POST /search/users credential probe",
	"cmd/findings.go:postFindingsList":                                "POST findings and audit searches",
}

// outOfScopeSelectors lists files (or package dirs) whose matches are not C1
// mutations through the shared client, one reason each.
var outOfScopeSelectors = map[string]string{
	"cmd/api.go":                          "the raw escape hatch",
	"cmd/auth_login_roles.go":             "cleans up the credential login created",
	"cmd/auth_logout.go":                  "keychain.Delete removes a local credential, not an HTTP call",
	"cmd/docs_openapi.go":                 "a GET Do that fetches the spec",
	"cmd/docs_search.go":                  "GETs of the public docs search and pages; no auth",
	"internal/selfupdate/":                "GETs the distribution index and manifests",
	"internal/tokensource/tokensource.go": "the OAuth token request; changes no C1 resource",
	"internal/keychain/keychain.go":       "keyring.Delete removes a local OS keyring entry",
}

// pendingMigration is Phase B's checklist: mutating commands that still send
// through the client directly. Remove an entry when its command moves onto
// sendMutation*; the test fails on an entry that already has.
var pendingMigration = map[string]bool{
	"c1i access-profiles bundle-automation create":        true,
	"c1i access-profiles bundle-automation delete":        true,
	"c1i access-profiles bundle-automation resume":        true,
	"c1i access-profiles bundle-automation run":           true,
	"c1i access-profiles bundle-automation set":           true,
	"c1i access-profiles create":                          true,
	"c1i access-profiles delete":                          true,
	"c1i access-profiles requestable-entitlements add":    true,
	"c1i access-profiles requestable-entitlements remove": true,
	"c1i access-profiles requestable-entitlements set":    true,
	"c1i access-profiles update":                          true,
	"c1i access-profiles visibility-entitlements add":     true,
	"c1i access-profiles visibility-entitlements remove":  true,
	"c1i accounts set-owner":                              true,
	"c1i automations delete":                              true,
	"c1i automations update":                              true,
	"c1i entitlements add-owner":                          true,
	"c1i entitlements create":                             true,
	"c1i entitlements delete":                             true,
	"c1i entitlements proxy-bindings create":              true,
	"c1i entitlements proxy-bindings delete":              true,
	"c1i entitlements remove-owner":                       true,
	"c1i entitlements update":                             true,
	"c1i mcp bindings create":                             true,
	"c1i mcp bindings delete":                             true,
	"c1i mcp classifiers bindings create":                 true,
	"c1i mcp classifiers bindings delete":                 true,
	"c1i mcp classifiers create":                          true,
	"c1i mcp classifiers delete":                          true,
	"c1i mcp classifiers policy update":                   true,
	"c1i mcp classifiers templates add-rule":              true,
	"c1i mcp classifiers templates instantiate":           true,
	"c1i mcp classifiers tool-gates create":               true,
	"c1i mcp classifiers tool-gates delete":               true,
	"c1i mcp classifiers tool-gates update":               true,
	"c1i mcp classifiers update":                          true,
	"c1i mcp servers delete":                              true,
	"c1i mcp servers register":                            true,
	"c1i mcp servers resync-tools":                        true,
	"c1i mcp servers update":                              true,
	"c1i mcp servers update-credentials":                  true,
	"c1i mcp tools approve":                               true,
	"c1i mcp tools delete":                                true,
	"c1i mcp toolsets create":                             true,
	"c1i mcp toolsets delete":                             true,
	"c1i mcp toolsets update":                             true,
	"c1i requests create grant":                           true,
	"c1i requests create revoke":                          true,
	"c1i service-principals bindings add":                 true,
	"c1i service-principals bindings delete":              true,
	"c1i service-principals create":                       true,
	"c1i service-principals credentials create":           true,
	"c1i service-principals credentials revoke":           true,
	"c1i service-principals credentials update":           true,
	"c1i service-principals delete":                       true,
	"c1i service-principals update":                       true,
	"c1i tasks approve":                                   true,
	"c1i tasks close":                                     true,
	"c1i tasks comment":                                   true,
	"c1i tasks deny":                                      true,
	"c1i tasks process":                                   true,
	"c1i tasks reassign":                                  true,
	"c1i tasks reset":                                     true,
	"c1i tasks restart":                                   true,
	"c1i tasks skip-step":                                 true,
	"c1i tasks update-grant-duration":                     true,
	"c1i webhooks create":                                 true,
	"c1i webhooks delete":                                 true,
	"c1i webhooks test":                                   true,
	"c1i webhooks update":                                 true,
}

// batchCommands maps each command calling sendMutationBatch to the id key
// every output line carries.
var batchCommands = map[string]string{}

// stderrPending holds files whose raw stderr writes move to the helper with
// their command's migration.
var stderrPending = map[string]string{
	"cmd/mcp_tools_approve.go": "per-id lines move to sendMutationBatch with mcp tools approve",
}

// ---- source index ----

type srcIndex struct {
	fset    *token.FileSet
	files   map[string]*ast.File // repo-relative path -> file
	funcs   map[string]*ast.FuncDecl
	methods map[string][]*ast.FuncDecl
	vars    map[string]ast.Expr // package var -> initializer (cmd only)
	declKey map[ast.Node]string // top-level decl -> file:decl
	byLine  map[string]map[int][]ast.Node
	fileOf  map[ast.Node]string
}

func loadSrcIndex(t *testing.T, extra map[string]string) *srcIndex {
	t.Helper()
	ix := &srcIndex{
		fset: token.NewFileSet(), files: map[string]*ast.File{}, funcs: map[string]*ast.FuncDecl{},
		methods: map[string][]*ast.FuncDecl{}, vars: map[string]ast.Expr{}, declKey: map[ast.Node]string{},
		byLine: map[string]map[int][]ast.Node{}, fileOf: map[ast.Node]string{},
	}
	var paths []string
	cmdFiles, _ := filepath.Glob("*.go")
	for _, p := range cmdFiles {
		paths = append(paths, "cmd/"+p)
	}
	_ = filepath.WalkDir("../internal", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".go") {
			paths = append(paths, strings.TrimPrefix(p, "../"))
		}
		return nil
	})
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(ix.fset, filepath.Join("..", p), nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", p, err)
		}
		ix.add(p, f)
	}
	for name, src := range extra {
		f, err := parser.ParseFile(ix.fset, name, src, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		ix.add(name, f)
	}
	return ix
}

func (ix *srcIndex) add(path string, f *ast.File) {
	ix.files[path] = f
	inCmd := strings.HasPrefix(path, "cmd/")
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			key := path + ":" + d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				recv := strings.TrimPrefix(types.ExprString(d.Recv.List[0].Type), "*")
				if i := strings.IndexByte(recv, '['); i >= 0 {
					recv = recv[:i]
				}
				key = fmt.Sprintf("%s:(%s).%s", path, recv, d.Name.Name)
				if inCmd {
					ix.methods[d.Name.Name] = append(ix.methods[d.Name.Name], d)
				}
			} else if inCmd {
				ix.funcs[d.Name.Name] = d
			}
			ix.declKey[d] = key
		case *ast.GenDecl:
			for _, s := range d.Specs {
				vs, ok := s.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, n := range vs.Names {
					ix.declKey[vs] = path + ":" + n.Name
					if inCmd && i < len(vs.Values) {
						ix.vars[n.Name] = vs.Values[i]
					}
				}
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.FuncLit, *ast.FuncDecl:
			line := ix.fset.Position(n.Pos()).Line
			if ix.byLine[path] == nil {
				ix.byLine[path] = map[int][]ast.Node{}
			}
			ix.byLine[path][line] = append(ix.byLine[path][line], n)
			ix.fileOf[n] = path
		}
		return true
	})
}

// enclosingKey names the top-level declaration containing pos.
func (ix *srcIndex) enclosingKey(path string, pos token.Pos) string {
	for node, key := range ix.declKey {
		if strings.HasPrefix(key, path+":") && node.Pos() <= pos && pos < node.End() {
			return key
		}
	}
	return path + ":?"
}

func isUnder(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if path == p || (strings.HasSuffix(p, "/") && strings.HasPrefix(path, p)) {
			return true
		}
	}
	return false
}

func outOfScope(path string) bool {
	for p := range outOfScopeSelectors {
		if path == p || (strings.HasSuffix(p, "/") && strings.HasPrefix(path, p)) {
			return true
		}
	}
	return false
}

// ---- closure walk ----

type finding struct {
	pos  token.Position
	what string
}

type closureReport struct {
	helpers   map[string]bool // sendMutation* reached directly
	rawWrites []finding       // mutating selectors outside the allowlists
	rawKeys   map[string]bool // their declarations
	forbidden []finding       // reads, waits and stdout writes outside withPreRead/withWait or the helper
	nonTail   []finding
}

type walker struct {
	ix   *srcIndex
	seen map[ast.Node]int // 1 = walked indirect, 2 = walked direct
	rep  *closureReport
}

// closureOf walks root and everything it references in package cmd: package
// funcs and vars by name (called or as values), cmd methods by name, and
// function-typed arguments. Arguments of withPreRead/withWait are walked as
// indirect, where reads and waits are allowed. The helper is not entered.
func (ix *srcIndex) closureOf(root ast.Node) *closureReport {
	w := &walker{ix: ix, seen: map[ast.Node]int{}, rep: &closureReport{helpers: map[string]bool{}, rawKeys: map[string]bool{}}}
	w.walk(root, true)
	return w.rep
}

func (w *walker) walk(root ast.Node, direct bool) {
	// A func that builds a waitOp or preRead is only usable through
	// withWait/withPreRead, so its reads are the helper's.
	if buildsHelperOp(root) {
		direct = false
	}
	level := 1
	if direct {
		level = 2
	}
	if w.seen[root] >= level {
		return
	}
	w.seen[root] = level
	path := w.ix.fileOf[root]
	if path == "" {
		for p, f := range w.ix.files {
			if f.Pos() <= root.Pos() && root.Pos() < f.End() {
				path = p
			}
		}
	}
	if path == "cmd/mutation.go" {
		return
	}
	locals := map[string]bool{}
	ast.Inspect(root, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok && len(as.Lhs) == len(as.Rhs) {
			for i, r := range as.Rhs {
				if id, ok := as.Lhs[i].(*ast.Ident); ok && isStdoutForm(r, nil) {
					locals[id.Name] = true
				}
			}
		}
		return true
	})
	var stack []ast.Node
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		stack = append(stack, n)
		pos := w.ix.fset.Position(n.Pos())
		switch x := n.(type) {
		case *ast.CallExpr:
			if id, ok := x.Fun.(*ast.Ident); ok && (id.Name == "withPreRead" || id.Name == "withWait") {
				for _, a := range x.Args {
					w.walk(a, false)
				}
				stack = stack[:len(stack)-1]
				return false
			}
			if id, ok := x.Fun.(*ast.Ident); ok && mutationHelpers[id.Name] {
				if direct {
					w.rep.helpers[id.Name] = true
				}
				if len(stack) < 2 || !isReturnOf(stack[len(stack)-2], x) {
					w.rep.nonTail = append(w.rep.nonTail, finding{pos, id.Name + " is not a tail call (return " + id.Name + "(...))"})
				}
			}
			if direct {
				if what := forbiddenCall(x, locals, w.ix); what != "" {
					w.rep.forbidden = append(w.rep.forbidden, finding{pos, what})
				}
			}
		case *ast.SelectorExpr:
			if mutatingSelectors[x.Sel.Name] && !isMethodConst(x) {
				key := w.ix.enclosingKey(path, x.Pos())
				if _, ro := readOnlyPOST[key]; !ro && !outOfScope(path) && !isUnder(path, chokepointFiles) {
					w.rep.rawWrites = append(w.rep.rawWrites, finding{pos, "." + x.Sel.Name})
					w.rep.rawKeys[key] = true
				}
			}
			if direct && x.Sel.Name == "Get" && !isCallFun(stack, x, 3) {
				if !isCallFunAny(stack, x) {
					w.rep.forbidden = append(w.rep.forbidden, finding{pos, "method value .Get"})
				}
			}
			for _, m := range w.ix.methods[x.Sel.Name] {
				w.walk(m, direct)
			}
		case *ast.Ident:
			if direct && (x.Name == "fetchOwnerIDs" || x.Name == "runWait") {
				w.rep.forbidden = append(w.rep.forbidden, finding{pos, x.Name + " outside withPreRead/withWait"})
			}
			if fd, ok := w.ix.funcs[x.Name]; ok && fd != root {
				w.walk(fd, direct)
			}
			if v, ok := w.ix.vars[x.Name]; ok {
				w.walk(v, direct)
			}
		}
		return true
	})
}

func buildsHelperOp(n ast.Node) bool {
	var ft *ast.FuncType
	switch f := n.(type) {
	case *ast.FuncLit:
		ft = f.Type
	case *ast.FuncDecl:
		ft = f.Type
	default:
		return false
	}
	if ft.Results == nil {
		return false
	}
	for _, r := range ft.Results.List {
		name := types.ExprString(r.Type)
		if strings.HasPrefix(name, "waitOp[") || name == "preRead" {
			return true
		}
	}
	return false
}

func isReturnOf(parent ast.Node, call *ast.CallExpr) bool {
	r, ok := parent.(*ast.ReturnStmt)
	return ok && len(r.Results) == 1 && r.Results[0] == call
}

// isMethodConst skips http.MethodPost-style names and package funcs that are
// not client methods.
func isMethodConst(x *ast.SelectorExpr) bool {
	id, ok := x.X.(*ast.Ident)
	return ok && (id.Name == "http" || id.Name == "strings" || id.Name == "os" || id.Name == "filepath")
}

func isCallFun(stack []ast.Node, sel *ast.SelectorExpr, nargs int) bool {
	if len(stack) < 2 {
		return false
	}
	c, ok := stack[len(stack)-2].(*ast.CallExpr)
	return ok && c.Fun == sel && len(c.Args) == nargs
}

func isCallFunAny(stack []ast.Node, sel *ast.SelectorExpr) bool {
	if len(stack) < 2 {
		return false
	}
	c, ok := stack[len(stack)-2].(*ast.CallExpr)
	return ok && c.Fun == sel
}

// isStdoutForm matches cmd.OutOrStdout(), os.Stdout, or a local holding one.
func isStdoutForm(e ast.Expr, locals map[string]bool) bool {
	switch x := e.(type) {
	case *ast.CallExpr:
		sel, ok := x.Fun.(*ast.SelectorExpr)
		return ok && sel.Sel.Name == "OutOrStdout"
	case *ast.SelectorExpr:
		pkg, ok := x.X.(*ast.Ident)
		return ok && pkg.Name == "os" && x.Sel.Name == "Stdout"
	case *ast.Ident:
		return locals[x.Name]
	}
	return false
}

func isStderrForm(e ast.Expr, locals map[string]bool) bool {
	switch x := e.(type) {
	case *ast.CallExpr:
		sel, ok := x.Fun.(*ast.SelectorExpr)
		return ok && sel.Sel.Name == "ErrOrStderr"
	case *ast.SelectorExpr:
		pkg, ok := x.X.(*ast.Ident)
		return ok && pkg.Name == "os" && x.Sel.Name == "Stderr"
	case *ast.Ident:
		return locals[x.Name]
	}
	return false
}

var outputFuncs = map[string]bool{"writeObject": true, "writeResource": true, "writeNamedResource": true, "writeRawObject": true, "newEmitter": true, "printDryRun": true}

// forbiddenCall reports a §5.2 output form or a read/wait in call.
func forbiddenCall(call *ast.CallExpr, locals map[string]bool, ix *srcIndex) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		if outputFuncs[fun.Name] {
			return fun.Name + " (mutation output goes through the helper)"
		}
		if fd, ok := ix.funcs[fun.Name]; ok {
			for i, a := range call.Args {
				if isStdoutForm(a, locals) && writesToParam(fd, i) {
					return fun.Name + " writes stdout through its writer parameter"
				}
			}
		}
	case *ast.SelectorExpr:
		pkg, _ := fun.X.(*ast.Ident)
		name := fun.Sel.Name
		switch {
		case pkg != nil && pkg.Name == "fmt" && strings.HasPrefix(name, "Fprint") && len(call.Args) > 0 && isStdoutForm(call.Args[0], locals),
			pkg != nil && pkg.Name == "io" && name == "WriteString" && len(call.Args) > 0 && isStdoutForm(call.Args[0], locals),
			pkg != nil && pkg.Name == "json" && name == "NewEncoder" && len(call.Args) > 0 && isStdoutForm(call.Args[0], locals):
			return pkg.Name + "." + name + " to stdout"
		case pkg != nil && pkg.Name == "fmt" && strings.HasPrefix(name, "Print"):
			return "fmt." + name + " (stdout)"
		case pkg != nil && pkg.Name == "cmd" && strings.HasPrefix(name, "Print"):
			return "cmd." + name
		case name == "Get" && len(call.Args) == 3:
			return "a .Get read outside withPreRead/withWait"
		}
	}
	return ""
}

// writesToParam reports whether fd Fprint*s to its i-th parameter.
func writesToParam(fd *ast.FuncDecl, i int) bool {
	var name string
	n := 0
	for _, f := range fd.Type.Params.List {
		for _, id := range f.Names {
			if n == i {
				name = id.Name
			}
			n++
		}
	}
	if name == "" {
		return false
	}
	found := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok || len(c.Args) == 0 {
			return true
		}
		if sel, ok := c.Fun.(*ast.SelectorExpr); ok {
			if pkg, ok := sel.X.(*ast.Ident); ok && (pkg.Name == "fmt" && strings.HasPrefix(sel.Sel.Name, "Fprint") || pkg.Name == "io" && sel.Sel.Name == "WriteString") {
				if id, ok := c.Args[0].(*ast.Ident); ok && id.Name == name {
					found = true
				}
			}
		}
		return true
	})
	return found
}

// ---- runtime command -> RunE source ----

type leafCmd struct {
	path string
	cmd  *cobra.Command
	root ast.Node
}

// skippedLeaves are not C1 REST commands, or are interactive.
var skippedLeaves = []string{"c1i completion", "c1i help"}

func leafCommands(t *testing.T, ix *srcIndex) []leafCmd {
	t.Helper()
	var out []leafCmd
	var visit func(c *cobra.Command)
	visit = func(c *cobra.Command) {
		for _, s := range c.Commands() {
			visit(s)
		}
		if c.HasSubCommands() || c.RunE == nil {
			return
		}
		p := c.CommandPath()
		for _, s := range skippedLeaves {
			if p == s || strings.HasPrefix(p, s+" ") {
				return
			}
		}
		fn := runtime.FuncForPC(reflect.ValueOf(c.RunE).Pointer())
		file, line := fn.FileLine(fn.Entry())
		rel := "cmd/" + filepath.Base(file)
		var root ast.Node
		for _, n := range ix.byLine[rel][line] {
			root = n
		}
		if root == nil && strings.HasSuffix(fn.Name(), "-fm") {
			root = ix.methodValue(fn.Name())
		}
		if root == nil {
			t.Fatalf("%s: RunE at %s:%d not found in the source index", p, rel, line)
		}
		out = append(out, leafCmd{p, c, root})
	}
	visit(rootCmd)
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

// methodValue resolves a method-value RunE ("pkg/cmd.(*T).m-fm" or
// "pkg/cmd.T.m-fm") to its declaration.
func (ix *srcIndex) methodValue(name string) ast.Node {
	name = strings.TrimSuffix(name, "-fm")
	name = name[strings.LastIndex(name, "/")+1:]
	name = strings.TrimPrefix(name, "cmd.")
	recv, method, ok := strings.Cut(name, ".")
	if !ok {
		return nil
	}
	recv = strings.Trim(recv, "(*)")
	for _, m := range ix.methods[method] {
		if strings.Contains(ix.declKey[m], ":("+recv+")."+method) {
			return m
		}
	}
	return nil
}

// ---- the tests ----

// TestMutationsWriteOnlyThroughHelper is Rule B: a command is mutating when
// its RunE closure reaches sendMutation*; it must be marked, tail-call the
// helper, and do no read, wait or output of its own. A command still sending
// a raw write must be in pendingMigration, and an entry there must still be.
func TestMutationsWriteOnlyThroughHelper(t *testing.T) {
	ix := loadSrcIndex(t, nil)
	leaves := leafCommands(t, ix)
	seen := map[string]bool{}
	batchSeen := map[string]bool{}
	for _, l := range leaves {
		rep := ix.closureOf(l.root)
		seen[l.path] = true
		batchSeen[l.path] = rep.helpers["sendMutationBatch"]
		for _, v := range leafViolations(l, rep) {
			t.Error(v)
		}
		// Self-test: tasks approve is mutating whether migrated or pending.
		if l.path == "c1i tasks approve" && len(rep.helpers) == 0 && len(rep.rawWrites) == 0 {
			t.Error("tasks approve is not classified as mutating; the classifier is broken")
		}
	}
	for p := range pendingMigration {
		if !seen[p] {
			t.Errorf("pendingMigration names %q, which is not a command", p)
		}
	}
	for p := range batchCommands {
		if !batchSeen[p] {
			t.Errorf("batchCommands names %q, which does not call sendMutationBatch", p)
		}
	}
}

// leafViolations applies Rule B to one command.
func leafViolations(l leafCmd, rep *closureReport) []string {
	var out []string
	migrated := len(rep.helpers) > 0
	raw := len(rep.rawWrites) > 0
	pending := pendingMigration[l.path]
	switch {
	case raw && !pending:
		out = append(out, fmt.Sprintf("%s sends a write outside cmd/mutation.go (%s at %s); use sendMutation", l.path, rep.rawWrites[0].what, rep.rawWrites[0].pos))
	case pending && !raw:
		out = append(out, fmt.Sprintf("%s is in pendingMigration but sends no raw write: remove the entry", l.path))
	}
	if migrated && !isMutating(l.cmd) {
		out = append(out, fmt.Sprintf("%s reaches %v but its init does not call markMutating", l.path, keys(rep.helpers)))
	}
	if isMutating(l.cmd) && !migrated && !pending {
		out = append(out, fmt.Sprintf("%s calls markMutating but never reaches sendMutation*", l.path))
	}
	if _, ok := batchCommands[l.path]; rep.helpers["sendMutationBatch"] && !ok {
		out = append(out, fmt.Sprintf("%s calls sendMutationBatch but is not in batchCommands", l.path))
	}
	if migrated {
		for _, f := range append(rep.nonTail, rep.forbidden...) {
			out = append(out, fmt.Sprintf("%s: %s at %s", l.path, f.what, f.pos))
		}
	}
	return out
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestMutatingSelectorsOnlyAtChokepoint is Rule A: outside the helper and the
// transport/protocol packages, a write selector may appear only in an
// allowlisted declaration or one a pending command reaches.
func TestMutatingSelectorsOnlyAtChokepoint(t *testing.T) {
	ix := loadSrcIndex(t, nil)
	pendingKeys := map[string]bool{}
	for _, l := range leafCommands(t, ix) {
		if pendingMigration[l.path] {
			for k := range ix.closureOf(l.root).rawKeys {
				pendingKeys[k] = true
			}
		}
	}
	for _, v := range ruleAViolations(ix, pendingKeys) {
		t.Error(v)
	}
	for _, v := range staleAllowlistEntries(ix) {
		t.Error(v)
	}
}

func ruleAViolations(ix *srcIndex, pendingKeys map[string]bool) []string {
	var out []string
	for path, f := range ix.files {
		if isUnder(path, chokepointFiles) || outOfScope(path) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			x, ok := n.(*ast.SelectorExpr)
			if !ok || !mutatingSelectors[x.Sel.Name] || isMethodConst(x) {
				return true
			}
			key := ix.enclosingKey(path, x.Pos())
			if _, ro := readOnlyPOST[key]; ro || pendingKeys[key] {
				return true
			}
			out = append(out, fmt.Sprintf("%s: .%s in %s outside cmd/mutation.go; send writes through sendMutation*", ix.fset.Position(x.Pos()), x.Sel.Name, key))
			return true
		})
	}
	sort.Strings(out)
	return out
}

// staleAllowlistEntries: every allowlist entry must still match a selector.
func staleAllowlistEntries(ix *srcIndex) []string {
	matched := map[string]bool{}
	for path, f := range ix.files {
		ast.Inspect(f, func(n ast.Node) bool {
			x, ok := n.(*ast.SelectorExpr)
			if !ok || !mutatingSelectors[x.Sel.Name] || isMethodConst(x) {
				return true
			}
			matched[ix.enclosingKey(path, x.Pos())] = true
			for p := range outOfScopeSelectors {
				if path == p || (strings.HasSuffix(p, "/") && strings.HasPrefix(path, p)) {
					matched[p] = true
				}
			}
			return true
		})
	}
	var out []string
	for k := range readOnlyPOST {
		if !matched[k] {
			out = append(out, "readOnlyPOST names "+k+", which has no write selector")
		}
	}
	for k := range outOfScopeSelectors {
		if !matched[k] {
			out = append(out, "outOfScopeSelectors names "+k+", which has no write selector")
		}
	}
	sort.Strings(out)
	return out
}

// TestStderrWritesOnlyThroughHelpers: under --error-format json every stderr
// line must be a JSON object, so only cmd/stderr.go and cmd/errors.go write it.
func TestStderrWritesOnlyThroughHelpers(t *testing.T) {
	ix := loadSrcIndex(t, nil)
	for _, v := range stderrViolations(ix) {
		t.Error(v)
	}
}

func stderrViolations(ix *srcIndex) []string {
	var out []string
	seenPending := map[string]bool{}
	for path, f := range ix.files {
		if !strings.HasPrefix(path, "cmd/") || path == "cmd/stderr.go" || path == "cmd/errors.go" {
			continue
		}
		locals := map[string]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			if as, ok := n.(*ast.AssignStmt); ok && len(as.Lhs) == len(as.Rhs) {
				for i, r := range as.Rhs {
					if id, ok := as.Lhs[i].(*ast.Ident); ok && isStderrForm(r, nil) {
						locals[id.Name] = true
					}
				}
			}
			return true
		})
		ast.Inspect(f, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok || len(c.Args) == 0 {
				return true
			}
			bad := ""
			switch fun := c.Fun.(type) {
			case *ast.SelectorExpr:
				pkg, _ := fun.X.(*ast.Ident)
				if pkg != nil && (pkg.Name == "fmt" && strings.HasPrefix(fun.Sel.Name, "Fprint") || pkg.Name == "io" && fun.Sel.Name == "WriteString") && isStderrForm(c.Args[0], locals) {
					bad = pkg.Name + "." + fun.Sel.Name
				}
			case *ast.Ident:
				if fd, ok := ix.funcs[fun.Name]; ok && !isUnder(ix.fileOf[fd], []string{"cmd/stderr.go", "cmd/errors.go"}) {
					for i, a := range c.Args {
						if isStderrForm(a, locals) && writesToParam(fd, i) {
							bad = fun.Name + " (through its writer parameter)"
						}
					}
				}
			}
			if bad == "" {
				return true
			}
			if _, ok := stderrPending[path]; ok {
				seenPending[path] = true
				return true
			}
			out = append(out, fmt.Sprintf("%s: %s to stderr; use notice/progress (cmd/stderr.go)", ix.fset.Position(c.Pos()), bad))
			return true
		})
	}
	for p := range stderrPending {
		if !seenPending[p] && ix.files[p] != nil {
			out = append(out, "stderrPending names "+p+", which no longer writes stderr directly")
		}
	}
	sort.Strings(out)
	return out
}
