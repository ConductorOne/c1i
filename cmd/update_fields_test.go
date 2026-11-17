package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// isUpdateCommand reports a runnable command named update or update-credentials.
func isUpdateCommand(c *cobra.Command) bool {
	name := strings.Fields(c.Use)[0]
	return c.Runnable() && (name == "update" || name == "update-credentials")
}

// TestEveryUpdateCommandDeclaresItsFields fails when an update command with
// field flags has no table, or registers a flag its table names neither as a field nor as a
// scope flag, or a table names a flag the command does not have.
func TestEveryUpdateCommandDeclaresItsFields(t *testing.T) {
	var walk func(c *cobra.Command)
	found := map[*cobra.Command]bool{}
	walk = func(c *cobra.Command) {
		if isUpdateCommand(c) {
			found[c] = true
			checkUpdateTables(t, c)
		}
		for _, s := range c.Commands() {
			walk(s)
		}
	}
	walk(rootCmd)
	if len(found) < 11 {
		t.Errorf("found %d update commands, want at least 11: the walk is broken", len(found))
	}
	for c := range updateSpecs {
		if !found[c] {
			t.Errorf("%s has a table but is not an update command", c.CommandPath())
		}
	}
}

func checkUpdateTables(t *testing.T, c *cobra.Command) {
	t.Helper()
	specs := updateSpecs[c]
	if len(specs) == 0 {
		// A body-file-only update sends the caller's object (and mask)
		// verbatim; a field flag needs a table.
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if f.Name != "help" && f.Name != "body-file" && f.Name != "update-mask" {
				t.Errorf("%s has --%s but no update table: register one with registerUpdateSpec", c.CommandPath(), f.Name)
			}
		})
		return
	}
	declared := map[string]bool{}
	for _, s := range specs {
		for _, n := range append(s.flagNames(), s.scope...) {
			declared[n] = true
			if c.Flags().Lookup(n) == nil {
				t.Errorf("%s: table names --%s, which the command does not register", c.CommandPath(), n)
			}
		}
	}
	c.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if f.Name != "help" && !declared[f.Name] {
			t.Errorf("%s: --%s is in no update table: add it as a field, or to scope if it is not one", c.CommandPath(), f.Name)
		}
	})
}

// TestNothingToUpdateErrorFromARealRun runs commands with no field flag and
// checks the printed error names the flags the old hand-written lists omitted.
func TestNothingToUpdateErrorFromARealRun(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"apps", "update", "app-1"}, "--unbind-policy"},
		{[]string{"entitlements", "update", "ent-1", "--app-id", "a1"}, "--duration-unset"},
		{[]string{"policies", "update", "pol-1"}, "or --body-file with --update-mask"},
	} {
		t.Setenv("C1I_URL", mutTestURL)
		r := runMutationCLI(t, nil, 0, nil, tc.args...)
		if r.exit != exitUsage || r.err == nil || !strings.Contains(r.err.Error(), "nothing to update: pass ") || !strings.Contains(r.err.Error(), tc.want) {
			t.Errorf("%v: exit %d, err %v; want exit 2 naming %q", tc.args, r.exit, r.err, tc.want)
		}
	}
}

func TestNothingToUpdateNamesEveryFlag(t *testing.T) {
	for c, specs := range updateSpecs {
		for _, s := range specs {
			msg := s.nothingToUpdate().Error()
			for _, n := range s.flagNames() {
				if !strings.Contains(msg, "--"+n) {
					t.Errorf("%s: %q omits --%s", c.CommandPath(), msg, n)
				}
			}
			if s.alt != "" && !strings.Contains(msg, s.alt) {
				t.Errorf("%s: %q omits %q", c.CommandPath(), msg, s.alt)
			}
		}
	}
}

// The mask reaches a request only through update_fields.go. Outside it, a
// string folding to "updateMask", a strings.Join over a mask-path variable, or
// a wrapUpdate/setUpdateMask argument that is not a string literal or a
// variable assigned only from a maskSources call is a hand-built mask.
var maskVarName = regexp.MustCompile(`(?i)^(paths|mask\w*|maskpaths)$`)

// maskSources are the calls whose result may be passed on as a mask.
// buildUpdateMask is trusted only as a method call (see isMaskSource) and
// declared only on updateSpec (TestUpdateMaskSourceIsDeclaredOnce).
var maskSources = map[string]bool{
	"buildUpdateMask": true, "bodyFileMask": true, "optionalUpdateMask": true,
	"deriveCredentialUpdateMask": true, "buildUpdatePolicyPatch": true,
}

// maskAllowedFuncs ("file:func") may join mask paths themselves: the one
// normalizer every --update-mask value goes through.
var maskAllowedFuncs = map[string]bool{"flags.go:normalizeUpdateMask": true}

// isMaskSource reports whether e is a call to a maskSources function; a bare
// call to the method's name is a look-alike function, not the updateSpec method.
func isMaskSource(e ast.Expr) bool {
	name := calleeName(e)
	if !maskSources[name] {
		return false
	}
	if name == "buildUpdateMask" {
		_, isMethod := e.(*ast.CallExpr).Fun.(*ast.SelectorExpr)
		return isMethod
	}
	return true
}

func calleeName(e ast.Expr) string {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return ""
	}
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	}
	return ""
}

// constString folds string literals joined by +.
func constString(e ast.Expr) (string, bool) {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return constString(e.X)
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(e.Value)
		return s, err == nil
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		l, lok := constString(e.X)
		r, rok := constString(e.Y)
		return l + r, lok && rok
	}
	return "", false
}

// maskAssignments records which identifiers in decl are assigned only from a
// string literal or a maskSources call.
func maskAssignments(decl ast.Node) (seen, bad map[string]bool) {
	seen, bad = map[string]bool{}, map[string]bool{}
	record := func(lhs []ast.Expr, rhs []ast.Expr, plain bool) {
		for i, l := range lhs {
			id, ok := l.(*ast.Ident)
			if !ok {
				continue
			}
			seen[id.Name] = true
			good := plain
			switch {
			case len(rhs) == 1 && isMaskSource(rhs[0]):
			case len(lhs) == len(rhs):
				_, good2 := constString(rhs[i])
				good = good && good2
			default:
				good = false
			}
			if !good {
				bad[id.Name] = true
			}
		}
	}
	ast.Inspect(decl, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			record(n.Lhs, n.Rhs, n.Tok == token.ASSIGN || n.Tok == token.DEFINE)
		case *ast.ValueSpec:
			if len(n.Values) > 0 {
				var lhs []ast.Expr
				for _, id := range n.Names {
					lhs = append(lhs, id)
				}
				record(lhs, n.Values, true)
			}
		}
		return true
	})
	return seen, bad
}

// handBuiltMasks returns the positions in src that build a mask by hand.
func handBuiltMasks(t *testing.T, name, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	report := func(p token.Pos, what string) {
		out = append(out, fset.Position(p).String()+": "+what)
	}
	for _, decl := range f.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && maskAllowedFuncs[name+":"+fd.Name.Name] {
			continue
		}
		seen, bad := maskAssignments(decl)
		ast.Inspect(decl, func(n ast.Node) bool {
			if fld, ok := n.(*ast.Field); ok && fld.Tag != nil {
				if tag, err := strconv.Unquote(fld.Tag.Value); err == nil {
					name, _, _ := strings.Cut(reflect.StructTag(tag).Get("json"), ",")
					if name == "updateMask" {
						report(fld.Tag.Pos(), `struct tag json:"updateMask"`)
					}
				}
			}
			if e, ok := n.(ast.Expr); ok {
				if s, ok := constString(e); ok && s == "updateMask" {
					report(e.Pos(), `string "updateMask"`)
					return false
				}
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch calleeName(call) {
			case "Join":
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && len(call.Args) == 2 {
					if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "strings" {
						if id, ok := call.Args[0].(*ast.Ident); ok && maskVarName.MatchString(id.Name) {
							report(call.Pos(), "strings.Join of "+id.Name)
						}
					}
				}
			case "wrapUpdate", "setUpdateMask":
				arg := call.Args[len(call.Args)-1]
				if _, ok := constString(arg); ok {
					return true
				}
				if id, ok := arg.(*ast.Ident); ok && seen[id.Name] && !bad[id.Name] {
					return true
				}
				report(arg.Pos(), "mask passed to "+calleeName(call)+" is not a literal or a spec/bodyFileMask result")
			}
			return true
		})
	}
	return out
}

// TestUpdateMaskSourceIsDeclaredOnce: the method name the guard trusts must
// not be declared anywhere else, or a look-alike method would be trusted too.
func TestUpdateMaskSourceIsDeclaredOnce(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Name.Name != "buildUpdateMask" {
				continue
			}
			found++
			if !declaresUpdateSpecMethod(fd) || name != "update_fields.go" {
				t.Errorf("%s: only (*updateSpec).buildUpdateMask in update_fields.go may use this name", name)
			}
		}
	}
	if found != 1 {
		t.Errorf("found %d declarations of buildUpdateMask, want 1", found)
	}
	for _, src := range []string{
		`package p; func buildUpdateMask() {}`,
		`package p; func (o *other) buildUpdateMask() {}`,
	} {
		f, _ := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
		if declaresUpdateSpecMethod(f.Decls[0].(*ast.FuncDecl)) {
			t.Errorf("look-alike accepted: %s", src)
		}
	}
}

func declaresUpdateSpecMethod(fd *ast.FuncDecl) bool {
	if fd.Recv == nil || len(fd.Recv.List) != 1 {
		return false
	}
	star, ok := fd.Recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	id, ok := star.X.(*ast.Ident)
	return ok && id.Name == "updateSpec"
}

// TestUpdateCommandsDoNotBuildMasksByHand keeps the mask in update_fields.go:
// a command declares fields in a table, or wraps a body-file's mask with
// wrapUpdate/setUpdateMask.
func TestUpdateCommandsDoNotBuildMasksByHand(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "update_fields.go" {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range handBuiltMasks(t, f, string(src)) {
			t.Errorf("%s: build update masks with an updateSpec (cmd/update_fields.go)", v)
		}
	}

	// Self-test: the checker must see each planted violation and spare the
	// sanctioned forms.
	planted := []string{
		`package p; func f(paths []string) any { return map[string]any{"updateMask": strings.Join(paths, ",")} }`,
		`package p; func f(b map[string]any) { b["updateMask"] = "x" }`,
		`package p; func f(maskParts []string) string { return strings.Join(maskParts, ",") }`,
		`package p; func f(o map[string]any, fieldPaths []string) any { return wrapUpdate("k", o, strings.Join(fieldPaths, ",")) }`,
		`package p; func f(b map[string]any, ps []string) { m := strings.Join(ps, ","); setUpdateMask(b, m) }`,
		`package p; func f(b map[string]any, s *updateSpec) any { m, _ := s.buildUpdateMask(nil, b); m += ",b"; return wrapUpdate("k", b, m) }`,
		`package p; const k = "update" + "Mask"`,
		"package p; type T struct { M string `json:\"updateMask,omitempty\"` }",
		`package p; func build(b map[string]any) string { return "a,b" }; func f(b map[string]any) any { m := build(b); return wrapUpdate("k", b, m) }`,
		`package p; func buildUpdateMask(b map[string]any) string { return "a,b" }; func f(b map[string]any) any { m := buildUpdateMask(b); return wrapUpdate("k", b, m) }`,
		`package p; func f(o map[string]any, m string) any { return wrapUpdate("k", o, m) }`,
	}
	for _, src := range planted {
		if len(handBuiltMasks(t, "planted.go", src)) == 0 {
			t.Errorf("the checker missed a planted violation: %s", src)
		}
	}
	clean := []string{
		`package p; func f(b map[string]any) any { return wrapUpdate("k", b, "m") }`,
		`package p; func f(parts []string) string { return strings.Join(parts, ",") }`,
		`package p; func f(b map[string]any, s *updateSpec) any { m, err := s.buildUpdateMask(nil, b); _ = err; return wrapUpdate("k", b, m) }`,
		`package p; func f(b map[string]any) any { m, _, _ := bodyFileMask(nil); return wrapUpdate("k", b, m) }`,
	}
	for _, src := range clean {
		if got := handBuiltMasks(t, "clean.go", src); len(got) != 0 {
			t.Errorf("the checker flagged sanctioned code %v: %s", got, src)
		}
	}
}
