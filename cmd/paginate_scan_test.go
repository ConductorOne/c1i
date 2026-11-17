package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// pagedPostAllowlist lists paths a postPage/postPageBody call may send that
// are not search- or list-shaped, one reason each. postPageBody is the one
// allowlisted POST selector in the mutation chokepoint test, so this keeps a
// mutating path from riding it.
var pagedPostAllowlist = map[string]string{
	"/api/v1/systemlog/events": "the audit-log export reads with a body filter",
}

// pagedPostIdents allows a path passed as a parameter the scan cannot
// resolve, keyed "file:ident".
var pagedPostIdents = map[string]string{
	"paginate.go:path": "adapter parameter; its callers are scanned at their own call sites",
	"service_principals_bindings.go:spBindingsListPath": "const /api/v1/service_principals/bindings/list, declared in another file",
}

func readShapedPath(p string) bool {
	return strings.Contains(p, "/search/") || strings.HasSuffix(p, "/search") || strings.HasSuffix(p, "/list") || pagedPostAllowlist[p] != ""
}

// firstStringLit returns the first string literal inside n.
func firstStringLit(n ast.Node) string {
	var out string
	ast.Inspect(n, func(m ast.Node) bool {
		if lit, ok := m.(*ast.BasicLit); ok && lit.Kind == token.STRING && out == "" {
			out = strings.Trim(lit.Value, "\"`")
		}
		return out == ""
	})
	return out
}

func isPagedPost(call *ast.CallExpr) bool {
	id, ok := call.Fun.(*ast.Ident)
	return ok && len(call.Args) >= 2 && (id.Name == "postPage" || id.Name == "postPageBody")
}

// badPagedPostPaths names postPage/postPageBody calls in file whose path is
// neither read-shaped nor allowlisted.
func badPagedPostPaths(fset *token.FileSet, file *ast.File, name string) []string {
	// An identifier resolves to the first string literal assigned to it.
	assigned := map[string]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			for i, l := range x.Lhs {
				if id, ok := l.(*ast.Ident); ok && i < len(x.Rhs) && assigned[id.Name] == "" {
					assigned[id.Name] = firstStringLit(x.Rhs[i])
				}
			}
		case *ast.ValueSpec:
			for i, id := range x.Names {
				if i < len(x.Values) && assigned[id.Name] == "" {
					assigned[id.Name] = firstStringLit(x.Values[i])
				}
			}
		}
		return true
	})
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isPagedPost(call) {
			return true
		}
		arg := call.Args[1]
		path := firstStringLit(arg)
		if id, ok := arg.(*ast.Ident); ok {
			if pagedPostIdents[name+":"+id.Name] != "" {
				return true
			}
			path = assigned[id.Name]
		}
		if !readShapedPath(path) {
			out = append(out, fset.Position(call.Pos()).String()+" "+path)
		}
		return true
	})
	return out
}

// TestPagedPostsOnlyReachReadPaths fails when postPage/postPageBody, which the
// mutation chokepoint test allowlists as read-only, is given a path that is
// not a search or list.
func TestPagedPostsOnlyReachReadPaths(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var calls int
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok && isPagedPost(c) {
				calls++
			}
			return true
		})
		for _, bad := range badPagedPostPaths(fset, file, path) {
			t.Errorf("%s: paged POST to a path that is not a search or list; send mutations through sendMutation", bad)
		}
	}
	if calls < 15 {
		t.Fatalf("saw only %d paged POST calls; this scan is not looking at what it thinks it is", calls)
	}
}

func TestPagedPostGuardCatchesMutatingPath(t *testing.T) {
	const planted = `package cmd

func f() {
	_ = postPage(ctx, "/api/v1/apps/x/delete", nil)
	_ = postPage(ctx, "/api/v1/search/apps", nil)
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "planted.go", planted, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := badPagedPostPaths(fset, file, "planted.go")
	if len(got) != 1 || !strings.Contains(got[0], "/delete") {
		t.Errorf("planted mutating path: got %v, want exactly the /delete call", got)
	}
}

// cursorLiteralAllowed names the functions outside cmd/paginate.go that may
// spell the "nextPageToken" key (a literal or a struct tag), keyed
// "file:func". Reading it anywhere else is a loop that should be a pageSpec.
var cursorLiteralAllowed = map[string]string{
	"api.go:nextPageTokenFromMap": "the raw api parser reads the cursor for runPages",
	"api.go:extractListAndToken":  "skips the cursor key when picking the list array",
}

// isCursorLiteral matches the key itself or a json struct tag naming it, not
// prose that mentions it.
func isCursorLiteral(lit *ast.BasicLit) bool {
	return lit.Kind == token.STRING && (strings.Trim(lit.Value, "\"`") == "nextPageToken" || strings.Contains(lit.Value, `json:"nextPageToken`))
}

func cursorLiteralUses(fset *token.FileSet, file *ast.File, name string) []string {
	var out []string
	for _, decl := range file.Decls {
		fn := "?"
		if fd, ok := decl.(*ast.FuncDecl); ok {
			fn = fd.Name.Name
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || !isCursorLiteral(lit) {
				return true
			}
			if cursorLiteralAllowed[name+":"+fn] == "" {
				out = append(out, fset.Position(lit.Pos()).String()+" "+lit.Value)
			}
			return true
		})
	}
	return out
}

// TestCursorKeyOnlyReadInPaginate fails on a "nextPageToken" literal or
// struct tag outside paginate.go and the api parsers: such code reads a
// cursor the stuck-cursor guard never sees.
func TestCursorKeyOnlyReadInPaginate(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") || path == "paginate.go" {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, use := range cursorLiteralUses(fset, file, path) {
			t.Errorf("%s: reads the next-page cursor outside cmd/paginate.go; express the list as a pageSpec", use)
		}
	}
	for key := range cursorLiteralAllowed {
		file, fn, _ := strings.Cut(key, ":")
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		var found bool
		for _, decl := range parsed.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name.Name == fn {
				ast.Inspect(fd, func(n ast.Node) bool {
					if lit, ok := n.(*ast.BasicLit); ok && isCursorLiteral(lit) {
						found = true
					}
					return true
				})
			}
		}
		if !found {
			t.Errorf("cursorLiteralAllowed has stale entry %s", key)
		}
	}
}

func TestCursorKeyGuardCatchesPlantedReads(t *testing.T) {
	const planted = "package cmd\n\nfunc a(page map[string]string) string { return page[\"nextPageToken\"] }\n\ntype resp struct {\n\tNext string `json:\"nextPageToken\"`\n}\n"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "planted.go", planted, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := cursorLiteralUses(fset, file, "planted.go"); len(got) != 2 {
		t.Errorf("planted reads: got %v, want the map index and the struct tag", got)
	}
}
