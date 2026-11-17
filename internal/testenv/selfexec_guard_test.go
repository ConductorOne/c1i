package testenv

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// importNames maps each import path to its local name in f.
func importNames(f *ast.File) map[string]string {
	m := map[string]string{}
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		name := p[strings.LastIndex(p, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}
		m[p] = name
	}
	return m
}

// selfExecViolations is a cheap early warning (the runtime block in
// testenv.Run is the real protection): a file that starts a process and also
// names os.Args or os.Executable, or that passes "go" ... "test" to a spawn.
func selfExecViolations(t *testing.T, name, src string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), name, src, 0)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	names := importNames(f)
	osN, execN, sysN := names["os"], names["os/exec"], names["syscall"]
	isSel := func(e ast.Expr, pkg, sel string) bool {
		x, ok := e.(*ast.SelectorExpr)
		if !ok || pkg == "" || pkg == "_" {
			return false
		}
		id, ok := x.X.(*ast.Ident)
		return ok && id.Name == pkg && (sel == "" || x.Sel.Name == sel)
	}
	var spawns, selfRef bool
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			isExec := isSel(x.Fun, execN, "Command") || isSel(x.Fun, execN, "CommandContext")
			if isExec || isSel(x.Fun, osN, "StartProcess") ||
				isSel(x.Fun, sysN, "Exec") || isSel(x.Fun, sysN, "ForkExec") || isSel(x.Fun, sysN, "StartProcess") {
				spawns = true
			}
			if isSel(x.Fun, osN, "Executable") {
				selfRef = true
			}
			if isExec {
				var lits []string
				for _, a := range x.Args {
					if bl, ok := a.(*ast.BasicLit); ok && bl.Kind == token.STRING {
						s, _ := strconv.Unquote(bl.Value)
						lits = append(lits, s)
					}
				}
				for i, l := range lits {
					if l == "go" && slices.Contains(lits[i+1:], "test") {
						out = append(out, "spawns `go test`")
						break
					}
				}
			}
		case *ast.CompositeLit:
			if isSel(x.Type, execN, "Cmd") {
				spawns = true
			}
		case *ast.SelectorExpr:
			if isSel(x, osN, "Args") {
				selfRef = true
			}
		}
		return true
	})
	if spawns && selfRef {
		out = append(out, "starts a process in a file that names os.Args or os.Executable()")
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
	const head = "package p\n\nimport (\n\t\"os\"\n\t\"os/exec\"\n\t\"syscall\"\n)\n\nvar _ = syscall.Getpid\n\n"
	const aliased = "package p\n\nimport (\n\tmyos \"os\"\n\trun \"os/exec\"\n)\n\n"
	for name, src := range map[string]string{
		"os.Args[0]":    head + "func f() { _ = exec.Command(os.Args[0], \"-test.run=X\") }\n",
		"os.Executable": head + "func f() { exe, _ := os.Executable(); _ = exec.CommandContext(nil, exe) }\n",
		"var then exec": head + "func f() { args := os.Args; _ = exec.Command(args[0]) }\n",
		"go test":       head + "func f() { _ = exec.Command(\"go\", \"test\", \"./...\") }\n",
		"go -C test":    head + "func f() { _ = exec.Command(\"go\", \"-C\", \"x\", \"test\") }\n",
		"aliased":       aliased + "func f() { _ = run.Command(myos.Args[0]) }\n",
		"syscall.Exec":  head + "func f() { _ = syscall.Exec(os.Args[0], nil, nil) }\n",
		"StartProcess":  head + "func f() { _, _ = os.StartProcess(os.Args[0], nil, nil) }\n",
		"Cmd literal":   head + "func f() { _ = exec.Cmd{Path: os.Args[0]} }\n",
	} {
		if len(selfExecViolations(t, "x_test.go", src)) == 0 {
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
