package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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

// A literal "updateMask" key, or strings.Join over a variable named like a
// mask-path list, outside update_fields.go is a hand-built mask.
var maskVarName = regexp.MustCompile(`(?i)^(paths|mask\w*|maskpaths)$`)

// handBuiltMasks returns the positions in src that build a mask by hand.
func handBuiltMasks(t *testing.T, name, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	isMaskKey := func(e ast.Expr) bool {
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return false
		}
		s, _ := strconv.Unquote(lit.Value)
		return s == "updateMask"
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.KeyValueExpr:
			if isMaskKey(n.Key) {
				out = append(out, fset.Position(n.Pos()).String()+": literal updateMask key")
			}
		case *ast.IndexExpr:
			if isMaskKey(n.Index) {
				out = append(out, fset.Position(n.Pos()).String()+": updateMask index")
			}
		case *ast.CallExpr:
			sel, ok := n.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Join" || len(n.Args) != 2 {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "strings" {
				return true
			}
			if id, ok := n.Args[0].(*ast.Ident); ok && maskVarName.MatchString(id.Name) {
				out = append(out, fset.Position(n.Pos()).String()+": strings.Join of "+id.Name)
			}
		}
		return true
	})
	return out
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
		if strings.HasSuffix(f, "_test.go") || f == "update_fields.go" || f == "flags.go" {
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
		`package p; func f(mask []string) string { return strings.Join(mask, ",") }`,
	}
	for _, src := range planted {
		if len(handBuiltMasks(t, "planted.go", src)) == 0 {
			t.Errorf("the checker missed a planted violation: %s", src)
		}
	}
	clean := []string{
		`package p; func f(b map[string]any) any { return wrapUpdate("k", b, "m") }`,
		`package p; func f(parts []string) string { return strings.Join(parts, ",") }`,
	}
	for _, src := range clean {
		if got := handBuiltMasks(t, "clean.go", src); len(got) != 0 {
			t.Errorf("the checker flagged sanctioned code %v: %s", got, src)
		}
	}
}
