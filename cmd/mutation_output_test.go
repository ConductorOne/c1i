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
// output (TestMutationsWriteOnlyThroughHelper and friends).

// readOnlyClientMethods are the exported *Client methods (internal/client,
// internal/transport) that cannot send a write. Every other exported method
// is a mutating selector, so a new one is caught until listed here.
var readOnlyClientMethods = map[string]string{
	"Get": "sends GET only",
}

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

// outOfScopeSelectors lists single calls ("file:decl selector") that are not
// C1 mutations through the shared client, one reason each.
var outOfScopeSelectors = map[string]string{
	"cmd/api.go:apiCmd c.Request":                                              "the raw escape hatch",
	"cmd/auth_login_roles.go:deletePersonalClient c.Delete":                    "cleans up the credential login created",
	"cmd/auth_logout.go:authLogoutCmd keychain.Delete":                         "removes a local credential, not an HTTP call",
	"cmd/docs_openapi.go:fetchOpenAPISpec hc.Do":                               "a GET that fetches the spec",
	"cmd/docs_search.go:docsSearchCmd http.DefaultClient.Do":                   "a GET of the public docs search; no auth",
	"cmd/docs_search.go:docsPageCmd http.DefaultClient.Do":                     "a GET of a public docs page; no auth",
	"internal/keychain/keychain.go:Delete keyring.Delete":                      "removes a local OS keyring entry",
	"internal/keychain/keychain.go:DeleteSecret keyring.Delete":                "removes a local OS keyring entry",
	"internal/keychain/keychain.go:storeKeyring keyring.Delete":                "rolls back a local OS keyring entry",
	"internal/selfupdate/dist.go:(Client).get doer.Do":                         "GETs the distribution index and manifests",
	"internal/selfupdate/verify.go:(tufFetcher).DownloadFile f.doer.Do":        "GETs signed release metadata",
	"internal/tokensource/tokensource.go:(c1TokenSource).Token c.transport.Do": "the OAuth token request; changes no C1 resource",
}

// batchCommands maps each command calling sendMutationBatch to the id key
// every output line carries.
var batchCommands = map[string]string{
	"c1i mcp tools approve": "id",
}

// nonMutatingOutputModes are declarations a mutating command reaches that
// write stdout in a mode that sends nothing. Their own bodies may write
// stdout; one reason each, and an entry that writes no stdout fails.
var nonMutatingOutputModes = map[string]string{
	"cmd/mcp_servers_register_template.go:printConfigTemplate": "--print-config-template prints a config skeleton and sends nothing",
}

// ---- source index ----

type srcIndex struct {
	fset     *token.FileSet
	files    map[string]*ast.File // repo-relative path -> file
	funcs    map[string]*ast.FuncDecl
	methods  map[string][]*ast.FuncDecl
	vars     map[string]ast.Expr // package var -> initializer (cmd only)
	declKey  map[ast.Node]string // top-level decl -> file:decl
	byLine   map[string]map[int][]ast.Node
	fileOf   map[ast.Node]string
	mutating map[string]bool // mutating selector names (mutatingMethods)
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
	ix.mutating = ix.mutatingMethods()
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

func outOfScopeKey(key string, x *ast.SelectorExpr) string {
	return key + " " + types.ExprString(x)
}

func outOfScope(_, key string, x *ast.SelectorExpr) bool {
	_, ok := outOfScopeSelectors[outOfScopeKey(key, x)]
	return ok
}

// isMutatingSelector: an exported *Client method not in
// readOnlyClientMethods, or Do on any receiver, as a call or a method value.
// Only net/http's Request type is exempt.
func (ix *srcIndex) isMutatingSelector(x *ast.SelectorExpr) bool {
	if !ix.mutating[x.Sel.Name] {
		return false
	}
	pkg, ok := x.X.(*ast.Ident)
	return !ok || pkg.Name != "http" || x.Sel.Name != "Request"
}

// mutatingMethods derives the mutating selector names from the exported API
// of the client packages.
func (ix *srcIndex) mutatingMethods() map[string]bool {
	out := map[string]bool{"Do": true}
	for path, f := range ix.files {
		if !isUnder(path, []string{"internal/client/", "internal/transport/"}) {
			continue
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || !fd.Name.IsExported() {
				continue
			}
			if types.ExprString(fd.Recv.List[0].Type) != "*Client" {
				continue
			}
			if _, ro := readOnlyClientMethods[fd.Name.Name]; !ro {
				out[fd.Name.Name] = true
			}
		}
	}
	return out
}

// ---- closure walk ----

type finding struct {
	pos  token.Position
	what string
}

type closureReport struct {
	helpers   map[string]bool // sendMutation* reached directly
	rawWrites []finding       // mutating selectors outside the allowlists
	forbidden []finding       // reads, waits and stdout writes outside withPreRead/withWait or the helper
	nonTail   []finding
}

type walkMode uint8

const (
	// modeDirect is the command's own code: no read, wait or stdout write.
	modeDirect walkMode = 1 << iota
	// modeInHelper is code the helper runs (a pre-read, a --wait op): it may
	// read through the client, but never write stdout or read an input file,
	// since it can run after the command already read stdin.
	modeInHelper
)

type walker struct {
	ix     *srcIndex
	seen   map[ast.Node]walkMode
	rep    *closureReport
	fields map[string]bool // ".field" names that hold a stdout writer somewhere in cmd
}

// closureOf walks root and everything it references in package cmd: package
// funcs and vars by name (called or as values), cmd methods by name, and
// function-typed arguments. Functions handed to withPreRead/withWait, and the
// closures a preRead/waitOp builder returns, are walked in modeInHelper. The
// helper is not entered.
func (ix *srcIndex) closureOf(root ast.Node) *closureReport {
	w := &walker{ix: ix, seen: map[ast.Node]walkMode{}, rep: &closureReport{helpers: map[string]bool{}}, fields: ix.stdoutFields()}
	w.walk(root, modeDirect)
	return w.rep
}

func (w *walker) walk(root ast.Node, mode walkMode) {
	w.walkWith(root, mode, helperOpClosures(root))
}

// walkWith walks root in mode, except for the FuncLits in deferred, which
// run inside the helper.
func (w *walker) walkWith(root ast.Node, mode walkMode, deferred map[ast.Node]bool) {
	if w.seen[root]&mode != 0 {
		return
	}
	w.seen[root] |= mode
	direct := mode == modeDirect
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
	locals := writerLocals(root, isStdoutForm)
	for f := range w.fields {
		locals[f] = true
	}
	_, exempt := nonMutatingOutputModes[w.ix.declKey[root]]
	var stack []ast.Node
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if n != root && deferred[n] {
			w.walk(n, modeInHelper)
			return false
		}
		stack = append(stack, n)
		pos := w.ix.fset.Position(n.Pos())
		switch x := n.(type) {
		case *ast.CallExpr:
			if id, ok := x.Fun.(*ast.Ident); ok && (id.Name == "withPreRead" || id.Name == "withWait") {
				for _, a := range x.Args {
					w.walkOpArg(a, mode)
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
			if what := forbiddenCall(x, locals, w.ix, exempt, mode); what != "" {
				w.rep.forbidden = append(w.rep.forbidden, finding{pos, what})
			}
		case *ast.SelectorExpr:
			if w.ix.isMutatingSelector(x) {
				key := w.ix.enclosingKey(path, x.Pos())
				if _, ro := readOnlyPOST[key]; !ro && !outOfScope(path, key, x) && !isUnder(path, chokepointFiles) {
					w.rep.rawWrites = append(w.rep.rawWrites, finding{pos, "." + x.Sel.Name})
				}
			}
			if direct && x.Sel.Name == "Get" && !isCallFun(stack, x, 3) {
				if !isCallFunAny(stack, x) {
					w.rep.forbidden = append(w.rep.forbidden, finding{pos, "method value .Get"})
				}
			}
			if mode == modeInHelper && x.Sel.Name == "InOrStdin" {
				w.rep.forbidden = append(w.rep.forbidden, finding{pos, "stdin read inside withPreRead/withWait (read input once, before the helper)"})
			}
			for _, m := range w.ix.methods[x.Sel.Name] {
				w.walk(m, mode)
			}
		case *ast.Ident:
			if direct && (x.Name == "fetchOwnerIDs" || x.Name == "runWait") {
				w.rep.forbidden = append(w.rep.forbidden, finding{pos, x.Name + " outside withPreRead/withWait"})
			}
			if mode == modeInHelper && inputReaders[x.Name] {
				w.rep.forbidden = append(w.rep.forbidden, finding{pos, x.Name + " inside withPreRead/withWait (read input once, before the helper)"})
			}
			if fd, ok := w.ix.funcs[x.Name]; ok && fd != root {
				w.walk(fd, mode)
			}
			if v, ok := w.ix.vars[x.Name]; ok {
				w.walk(v, mode)
			}
		}
		return true
	})
}

// inputReaders read a --*-file flag or stdin.
var inputReaders = map[string]bool{"readJSONFile": true, "readJSONBytes": true}

// walkOpArg walks an argument of withPreRead/withWait. A function value runs
// inside the helper; anything else is evaluated now, in mode, except the
// function literals it carries.
func (w *walker) walkOpArg(a ast.Expr, mode walkMode) {
	switch x := a.(type) {
	case *ast.FuncLit:
		w.walk(x, modeInHelper)
		return
	case *ast.Ident:
		if fd, ok := w.ix.funcs[x.Name]; ok {
			w.walk(fd, modeInHelper)
			return
		}
	}
	w.walkWith(a, mode, outerFuncLits(a))
}

// outerFuncLits are the FuncLits in n not nested in another FuncLit.
func outerFuncLits(n ast.Node) map[ast.Node]bool {
	out := map[ast.Node]bool{}
	ast.Inspect(n, func(m ast.Node) bool {
		if fl, ok := m.(*ast.FuncLit); ok && m != n {
			out[fl] = true
			return false
		}
		return true
	})
	return out
}

// helperOpClosures are the FuncLits a preRead/waitOp builder returns. Only
// they run inside the helper; the builder's own body runs when it is called.
func helperOpClosures(n ast.Node) map[ast.Node]bool {
	var body *ast.BlockStmt
	switch f := n.(type) {
	case *ast.FuncLit:
		body = f.Body
	case *ast.FuncDecl:
		body = f.Body
	}
	out := map[ast.Node]bool{}
	if !buildsHelperOp(n) || body == nil {
		return out
	}
	ast.Inspect(body, func(m ast.Node) bool {
		switch m := m.(type) {
		case *ast.FuncLit:
			return false // its returns are its own
		case *ast.ReturnStmt:
			for _, r := range m.Results {
				for fl := range outerFuncLits(r) {
					out[fl] = true
				}
				if fl, ok := r.(*ast.FuncLit); ok {
					out[fl] = true
				}
			}
		}
		return true
	})
	return out
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

// writerForm matches a stream: a cobra accessor call, os.<file>, or a local
// or field (keyed ".name") holding one.
func writerForm(e ast.Expr, locals map[string]bool, accessors map[string]bool, osFile string) bool {
	switch x := e.(type) {
	case *ast.CallExpr:
		sel, ok := x.Fun.(*ast.SelectorExpr)
		return ok && accessors[sel.Sel.Name]
	case *ast.SelectorExpr:
		if locals["."+x.Sel.Name] {
			return true
		}
		pkg, ok := x.X.(*ast.Ident)
		return ok && pkg.Name == "os" && x.Sel.Name == osFile
	case *ast.Ident:
		return locals[x.Name]
	case *ast.ParenExpr:
		return writerForm(x.X, locals, accessors, osFile)
	}
	return false
}

// OutOrStderr is the command's out writer too: it is stdout once SetOut is.
func isStdoutForm(e ast.Expr, locals map[string]bool) bool {
	return writerForm(e, locals, map[string]bool{"OutOrStdout": true, "OutOrStderr": true}, "Stdout")
}

func isStderrForm(e ast.Expr, locals map[string]bool) bool {
	return writerForm(e, locals, map[string]bool{"ErrOrStderr": true}, "Stderr")
}

// writerLocals names the locals in root assigned (:=, =, or var) from a
// stream form, and the fields (".name") assigned or keyed from one.
func writerLocals(root ast.Node, form func(ast.Expr, map[string]bool) bool) map[string]bool {
	locals := map[string]bool{}
	ast.Inspect(root, func(n ast.Node) bool {
		var lhs []string
		var rhs []ast.Expr
		switch x := n.(type) {
		case *ast.AssignStmt:
			for _, l := range x.Lhs {
				lhs = append(lhs, writerHolder(l))
			}
			rhs = x.Rhs
		case *ast.ValueSpec:
			for _, id := range x.Names {
				lhs = append(lhs, id.Name)
			}
			rhs = x.Values
		case *ast.KeyValueExpr:
			if k, ok := x.Key.(*ast.Ident); ok {
				lhs, rhs = []string{"." + k.Name}, []ast.Expr{x.Value}
			}
		}
		if len(lhs) != len(rhs) {
			return true
		}
		for i, r := range rhs {
			if lhs[i] != "" && form(r, nil) {
				locals[lhs[i]] = true
			}
		}
		return true
	})
	return locals
}

func writerHolder(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return "." + x.Sel.Name
	}
	return ""
}

// stdoutFields are the fields assigned a stdout writer anywhere in cmd
// outside the helper; a method can write through one set elsewhere.
func (ix *srcIndex) stdoutFields() map[string]bool {
	out := map[string]bool{}
	for path, f := range ix.files {
		if !strings.HasPrefix(path, "cmd/") || path == "cmd/mutation.go" {
			continue
		}
		for k := range writerLocals(f, isStdoutForm) {
			if strings.HasPrefix(k, ".") {
				out[k] = true
			}
		}
	}
	return out
}

// writerMethods write to their receiver.
var writerMethods = map[string]bool{"Write": true, "WriteString": true, "WriteByte": true, "WriteRune": true, "ReadFrom": true}

// writesTo reports how call writes to a stream that is matches: a write
// method on it, handing it to anything outside package cmd (fmt.Fprint*,
// io.Copy, json.NewEncoder, bufio.NewWriter, …), or passing it to a cmd
// func that does either with that parameter. trusted cmd funcs may.
func writesTo(call *ast.CallExpr, is func(ast.Expr) bool, ix *srcIndex, trusted func(*ast.FuncDecl) bool, depth int) string {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok && writerMethods[sel.Sel.Name] && is(sel.X) {
		return "." + sel.Sel.Name
	}
	var fd *ast.FuncDecl
	if id, ok := call.Fun.(*ast.Ident); ok {
		fd = ix.funcs[id.Name]
	}
	for i, a := range call.Args {
		switch {
		case !is(a):
		case fd == nil:
			return types.ExprString(call.Fun)
		case trusted(fd):
		case depth < 4 && writesToParam(fd, i, ix, trusted, depth+1):
			return fd.Name.Name + " writes"
		}
	}
	return ""
}

// writesToParam reports whether fd writes to its i-th parameter.
func writesToParam(fd *ast.FuncDecl, i int, ix *srcIndex, trusted func(*ast.FuncDecl) bool, depth int) bool {
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
	if name == "" || fd.Body == nil {
		return false
	}
	is := func(e ast.Expr) bool {
		id, ok := e.(*ast.Ident)
		return ok && id.Name == name
	}
	found := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok && !found && writesTo(c, is, ix, trusted, depth) != "" {
			found = true
		}
		return !found
	})
	return found
}

func untrusted(*ast.FuncDecl) bool { return false }

var outputFuncs = map[string]bool{"writeObject": true, "writeResource": true, "writeNamedResource": true, "writeRawObject": true, "newEmitter": true, "printDryRun": true}

// forbiddenCall reports a §5.2 output form in call, and in modeDirect a
// read too. An exempt declaration (nonMutatingOutputModes) may write stdout.
func forbiddenCall(call *ast.CallExpr, locals map[string]bool, ix *srcIndex, exempt bool, mode walkMode) string {
	if id, ok := call.Fun.(*ast.Ident); ok && outputFuncs[id.Name] {
		return id.Name + " (mutation output goes through the helper)"
	}
	if !exempt {
		if how := writesTo(call, func(e ast.Expr) bool { return isStdoutForm(e, locals) }, ix, untrusted, 0); how != "" {
			if strings.HasSuffix(how, " writes") {
				return how + " stdout through its writer parameter"
			}
			return how + " to stdout"
		}
	}
	if fun, ok := call.Fun.(*ast.SelectorExpr); ok {
		pkg, _ := fun.X.(*ast.Ident)
		name := fun.Sel.Name
		switch {
		case exempt:
		case pkg != nil && pkg.Name == "fmt" && strings.HasPrefix(name, "Print"):
			return "fmt." + name + " (stdout)"
		case pkg != nil && pkg.Name == "cmd" && strings.HasPrefix(name, "Print"):
			return "cmd." + name
		}
		if mode == modeDirect && name == "Get" && len(call.Args) == 3 {
			return "a .Get read outside withPreRead/withWait"
		}
	}
	return ""
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
// helper, and do no read, wait or output of its own.
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
		// Self-test: the classifier must see tasks approve as mutating.
		if l.path == "c1i tasks approve" && len(rep.helpers) == 0 {
			t.Error("tasks approve is not classified as mutating; the classifier is broken")
		}
	}
	for _, v := range staleOutputExemptions(ix) {
		t.Error(v)
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
	if len(rep.rawWrites) > 0 {
		out = append(out, fmt.Sprintf("%s sends a write outside cmd/mutation.go (%s at %s); use sendMutation", l.path, rep.rawWrites[0].what, rep.rawWrites[0].pos))
	}
	if migrated && !isMutating(l.cmd) {
		out = append(out, fmt.Sprintf("%s reaches %v but its init does not call markMutating", l.path, keys(rep.helpers)))
	}
	if isMutating(l.cmd) && !migrated {
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
// allowlisted declaration.
func TestMutatingSelectorsOnlyAtChokepoint(t *testing.T) {
	ix := loadSrcIndex(t, nil)
	for _, v := range ruleAViolations(ix) {
		t.Error(v)
	}
	for _, v := range staleAllowlistEntries(ix) {
		t.Error(v)
	}
}

func ruleAViolations(ix *srcIndex) []string {
	var out []string
	for path, f := range ix.files {
		if isUnder(path, chokepointFiles) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			x, ok := n.(*ast.SelectorExpr)
			if !ok || !ix.isMutatingSelector(x) {
				return true
			}
			key := ix.enclosingKey(path, x.Pos())
			if _, ro := readOnlyPOST[key]; ro || outOfScope(path, key, x) {
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
			if !ok || !ix.isMutatingSelector(x) {
				return true
			}
			key := ix.enclosingKey(path, x.Pos())
			matched[key] = true
			matched[outOfScopeKey(key, x)] = true
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
	stderrFiles := []string{"cmd/stderr.go", "cmd/errors.go"}
	trusted := func(fd *ast.FuncDecl) bool { return isUnder(ix.fileOf[fd], stderrFiles) }
	var out []string
	for path, f := range ix.files {
		if !strings.HasPrefix(path, "cmd/") || isUnder(path, stderrFiles) {
			continue
		}
		locals := writerLocals(f, isStderrForm)
		is := func(e ast.Expr) bool { return isStderrForm(e, locals) }
		ast.Inspect(f, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if how := writesTo(c, is, ix, trusted, 0); how != "" {
				out = append(out, fmt.Sprintf("%s: %s to stderr in %s; use notice/progress (cmd/stderr.go)", ix.fset.Position(c.Pos()), how, ix.enclosingKey(path, c.Pos())))
			}
			return true
		})
	}
	sort.Strings(out)
	return out
}

// staleOutputExemptions: every nonMutatingOutputModes entry must still name a
// declaration that writes stdout.
func staleOutputExemptions(ix *srcIndex) []string {
	var out []string
	for key := range nonMutatingOutputModes {
		writes := false
		for n, k := range ix.declKey {
			if k != key {
				continue
			}
			locals := writerLocals(n, isStdoutForm)
			ast.Inspect(n, func(m ast.Node) bool {
				if c, ok := m.(*ast.CallExpr); ok && writesTo(c, func(e ast.Expr) bool { return isStdoutForm(e, locals) }, ix, untrusted, 0) != "" {
					writes = true
				}
				return !writes
			})
		}
		if !writes {
			out = append(out, "nonMutatingOutputModes names "+key+", which writes no stdout")
		}
	}
	sort.Strings(out)
	return out
}

// rawUnwrapUsers may print a response unchanged (§3.4: the only sibling
// collision); every other command unwraps like its family's read.
var rawUnwrapUsers = map[string]bool{"cmd/findings.go:findingsCreateTaskCmd": true}

func rawUnwrapViolations(ix *srcIndex) []string {
	var out []string
	for path, f := range ix.files {
		if path == "cmd/mutation.go" {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "raw" && len(c.Args) == 0 {
				if key := ix.enclosingKey(path, c.Pos()); !rawUnwrapUsers[key] {
					out = append(out, fmt.Sprintf("%s: raw() in %s; only findings create-task prints its response unwrapped", ix.fset.Position(c.Pos()), key))
				}
			}
			return true
		})
	}
	sort.Strings(out)
	return out
}

func TestOnlyFindingsCreateTaskUsesRawUnwrap(t *testing.T) {
	for _, v := range rawUnwrapViolations(loadSrcIndex(t, nil)) {
		t.Error(v)
	}
}
