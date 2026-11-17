package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// Guards for the two ways a cmd test leaks package-level state into the next
// one under -shuffle:
//   - restoring viper with viper.Set in a Cleanup/defer leaves an override that
//     beats --dry-run, C1I_DRY_RUN and --fields; clearViperOverrides drops it.
//   - rootCmd.Execute* directly pins the test's context (cancelled on return)
//     on every leaf it reaches; executeRoot* reset them first.

// rootExecuteAllowlist names the funcs that may call rootCmd.Execute*.
var rootExecuteAllowlist = map[string]string{
	"executeRootCtx": "the one helper; it sets a live context on every command first",
}

var rootExecuteMethods = map[string]bool{
	"Execute": true, "ExecuteContext": true, "ExecuteC": true, "ExecuteContextC": true,
}

func isSel(e ast.Expr, recv, name string) bool {
	s, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := s.X.(*ast.Ident)
	return ok && id.Name == recv && (name == "" || s.Sel.Name == name)
}

// restoreViaViperSet returns "file:line" for each viper.Set(k, non-nil) inside
// a t.Cleanup func literal or a defer. viper.Set(k, nil) is how an override is
// dropped, so it is the one allowed call.
func restoreViaViperSet(fset *token.FileSet, f *ast.File) []string {
	var out []string
	scan := func(root ast.Node) {
		ast.Inspect(root, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok || !isSel(c.Fun, "viper", "Set") || len(c.Args) != 2 {
				return true
			}
			if id, ok := c.Args[1].(*ast.Ident); ok && id.Name == "nil" {
				return true
			}
			out = append(out, fset.Position(c.Pos()).String())
			return true
		})
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.DeferStmt:
			scan(n.Call)
		case *ast.CallExpr:
			if s, ok := n.Fun.(*ast.SelectorExpr); ok && s.Sel.Name == "Cleanup" {
				for _, a := range n.Args {
					if lit, ok := a.(*ast.FuncLit); ok {
						scan(lit)
					}
				}
			}
		}
		return true
	})
	return out
}

// directRootExecutes returns "file:line" for each rootCmd.Execute* call
// outside an allowlisted func.
func directRootExecutes(fset *token.FileSet, f *ast.File) []string {
	var out []string
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if _, ok := rootExecuteAllowlist[fd.Name.Name]; ok {
			continue
		}
		ast.Inspect(fd, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok && isSel(c.Fun, "rootCmd", "") &&
				rootExecuteMethods[c.Fun.(*ast.SelectorExpr).Sel.Name] {
				out = append(out, fset.Position(c.Pos()).String())
			}
			return true
		})
	}
	return out
}

func scanTestFiles(t *testing.T, check func(*token.FileSet, *ast.File) []string) []string {
	t.Helper()
	files, err := filepath.Glob("*_test.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no test files found (err %v); this guard is not looking at what it thinks it is", err)
	}
	var out []string
	for _, name := range files {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, check(fset, f)...)
	}
	return out
}

func TestNoViperRestoreInCleanupOrDefer(t *testing.T) {
	for _, v := range scanTestFiles(t, restoreViaViperSet) {
		t.Errorf("%s: viper.Set in a Cleanup/defer leaves an override that beats --dry-run/--fields in later tests; use clearViperOverrides(t, key)", v)
	}
}

func TestNoDirectRootExecuteInTests(t *testing.T) {
	for _, v := range scanTestFiles(t, directRootExecutes) {
		t.Errorf("%s: call executeRoot/executeRootC/executeRootCtx (geturl_test.go), not rootCmd.Execute*: cobra pins the first context on each leaf", v)
	}
	for name := range rootExecuteAllowlist {
		if len(scanTestFiles(t, func(_ *token.FileSet, f *ast.File) []string {
			var found []string
			for _, d := range f.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == name {
					found = append(found, name)
				}
			}
			return found
		})) == 0 {
			t.Errorf("allowlisted func %s no longer exists; drop it from rootExecuteAllowlist", name)
		}
	}
}

func parseSnippet(t *testing.T, src string) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "snippet_test.go", "package cmd\n"+src, 0)
	if err != nil {
		t.Fatal(err)
	}
	return fset, f
}

func TestStateLeakGuardsCatchPlantedViolations(t *testing.T) {
	viperBad := []string{
		`func TestX(t *testing.T) { t.Cleanup(func() { viper.Set("dry_run", orig) }) }`,
		`func TestX(t *testing.T) { defer func() { viper.Set("fields", "") }() }`,
		`func TestX(t *testing.T) { defer viper.Set("url", orig) }`,
		`func TestX(t *testing.T) { t.Run("a", func(t *testing.T) { t.Cleanup(func() { viper.Set("url", "x") }) }) }`,
	}
	for _, src := range viperBad {
		fset, f := parseSnippet(t, src)
		if len(restoreViaViperSet(fset, f)) == 0 {
			t.Errorf("viper guard missed: %s", src)
		}
	}
	viperOK := []string{
		`func TestX(t *testing.T) { viper.Set("dry_run", true); t.Cleanup(func() { viper.Set("dry_run", nil) }) }`,
		`func TestX(t *testing.T) { viper.Set("dry_run", true) }`,
	}
	for _, src := range viperOK {
		fset, f := parseSnippet(t, src)
		if got := restoreViaViperSet(fset, f); len(got) != 0 {
			t.Errorf("viper guard flagged a legal use %v: %s", got, src)
		}
	}

	for _, m := range []string{"Execute()", "ExecuteContext(ctx)", "ExecuteC()", "ExecuteContextC(ctx)"} {
		src := `func TestX(t *testing.T) { _ = rootCmd.` + m + ` }`
		fset, f := parseSnippet(t, src)
		if len(directRootExecutes(fset, f)) == 0 {
			t.Errorf("execute guard missed: %s", src)
		}
	}
	for _, src := range []string{
		`func executeRootCtx(ctx context.Context) { _ = rootCmd.ExecuteContextC(ctx) }`,
		`func TestX(t *testing.T) { _ = leafCmd.Execute() }`,
	} {
		fset, f := parseSnippet(t, src)
		if got := directRootExecutes(fset, f); len(got) != 0 {
			t.Errorf("execute guard flagged a legal use %v: %s", got, src)
		}
	}
}
