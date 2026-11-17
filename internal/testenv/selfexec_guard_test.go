package testenv

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// selfExecViolations reports why src (a _test.go file) may fork the test
// binary: a file that runs a subprocess and also names os.Args[0] or
// os.Executable(), or that spawns `go test`. A runaway child once forced a
// host reboot.
func selfExecViolations(t *testing.T, name, src string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), name, src, 0)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var spawns, selfRef bool
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			sel, ok := x.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			switch {
			case pkg.Name == "exec" && (sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext"):
				spawns = true
				var lits []string
				for _, a := range x.Args {
					if bl, ok := a.(*ast.BasicLit); ok && bl.Kind == token.STRING {
						s, _ := strconv.Unquote(bl.Value)
						lits = append(lits, s)
					}
				}
				for i := 0; i+1 < len(lits); i++ {
					if lits[i] == "go" && lits[i+1] == "test" {
						out = append(out, "spawns `go test`")
					}
				}
			case pkg.Name == "os" && sel.Sel.Name == "Executable":
				selfRef = true
			}
		case *ast.IndexExpr:
			if sel, ok := x.X.(*ast.SelectorExpr); ok {
				if p, ok := sel.X.(*ast.Ident); ok && p.Name == "os" && sel.Sel.Name == "Args" {
					selfRef = true
				}
			}
		}
		return true
	})
	if spawns && selfRef {
		out = append(out, "runs a subprocess in a file that names os.Args[...] or os.Executable()")
	}
	return out
}

func TestNoTestExecsTheTestBinary(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".claude", "dev", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		checked++
		rel, _ := filepath.Rel(root, path)
		for _, v := range selfExecViolations(t, rel, string(src)) {
			t.Errorf("%s: %s; run the code in-process instead", rel, v)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 50 {
		t.Errorf("checked only %d test files", checked)
	}
}

func TestSelfExecGuardCatchesPlantedViolations(t *testing.T) {
	const head = "package p\n\nimport (\n\t\"os\"\n\t\"os/exec\"\n)\n\n"
	for name, body := range map[string]string{
		"os.Args[0]":    "func f() { _ = exec.Command(os.Args[0], \"-test.run=X\") }\n",
		"os.Executable": "func f() { exe, _ := os.Executable(); _ = exec.CommandContext(nil, exe) }\n",
		"var then exec": "func f() { a := os.Args[0]; _ = exec.Command(a) }\n",
		"go test":       "func f() { _ = exec.Command(\"go\", \"test\", \"./...\") }\n",
	} {
		if len(selfExecViolations(t, "x_test.go", head+body)) == 0 {
			t.Errorf("%s: planted violation not caught", name)
		}
	}
	for name, body := range map[string]string{
		"git":     "func f() { _ = exec.Command(\"git\", \"ls-files\") }\n",
		"go list": "func f() { _ = exec.Command(\"go\", \"list\", \"./...\") }\n",
		"no exec": "func f() { _ = os.Args }\n",
	} {
		if v := selfExecViolations(t, "x_test.go", head+body); len(v) != 0 {
			t.Errorf("%s: false positive %v", name, v)
		}
	}
}
