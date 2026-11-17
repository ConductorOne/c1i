package testenv

import (
	"encoding/json"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const modPath = "github.com/ConductorOne/c1i/"

// hasTestenvMain reports whether the package in dir has, in a test file that
// builds under ctx, a TestMain whose body calls testenv.Run.
func hasTestenvMain(t *testing.T, ctx build.Context, dir string) bool {
	t.Helper()
	pkg, err := ctx.ImportDir(dir, 0)
	if err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	for _, name := range slices.Concat(pkg.TestGoFiles, pkg.XTestGoFiles) {
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		local := ""
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == modPath+"internal/testenv" {
				local = "testenv"
				if imp.Name != nil {
					local = imp.Name.Name
				}
			}
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name.Name != "TestMain" || fn.Body == nil {
				continue
			}
			found := false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch fun := call.Fun.(type) {
				case *ast.SelectorExpr: // testenv.Run(m)
					if x, ok := fun.X.(*ast.Ident); ok && local != "" && x.Name == local && fun.Sel.Name == "Run" {
						found = true
					}
				case *ast.Ident: // Run(m), inside package testenv itself
					if f.Name.Name == "testenv" && fun.Name == "Run" {
						found = true
					}
				}
				return !found
			})
			if found {
				return true
			}
		}
	}
	return false
}

func TestHasTestenvMain(t *testing.T) {
	const head = "package p\n\nimport (\n\t\"os\"\n\t\"testing\"\n\n\t\"" + modPath + "internal/testenv\"\n)\n\n"
	cases := []struct {
		name  string
		files map[string]string
		want  bool
	}{
		{"calls testenv.Run", map[string]string{"main_test.go": head + "func TestMain(m *testing.M) { os.Exit(testenv.Run(m)) }\n"}, true},
		{"only in a comment", map[string]string{"main_test.go": head + "// testenv.Run(m)\nfunc TestMain(m *testing.M) { os.Exit(m.Run()) }\n"}, false},
		{"only in a string", map[string]string{"main_test.go": head + "func TestMain(m *testing.M) { _ = \"testenv.Run(\"; os.Exit(m.Run()) }\n"}, false},
		{"Run outside TestMain", map[string]string{"main_test.go": head + "func helper(m *testing.M) int { return testenv.Run(m) }\nfunc TestMain(m *testing.M) { os.Exit(m.Run()) }\n"}, false},
		{"TestMain only in a windows file", map[string]string{
			"main_windows_test.go": head + "func TestMain(m *testing.M) { os.Exit(testenv.Run(m)) }\n",
			"x_test.go":            "package p\n",
		}, false},
		{"TestMain behind a windows build tag", map[string]string{
			"main_test.go": "//go:build windows\n\n" + head + "func TestMain(m *testing.M) { os.Exit(testenv.Run(m)) }\n",
			"x_test.go":    "package p\n",
		}, false},
	}
	ctx := build.Default
	ctx.GOOS, ctx.GOARCH, ctx.CgoEnabled = "linux", "amd64", false
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for n, src := range tc.files {
				if err := os.WriteFile(filepath.Join(dir, n), []byte(src), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if got := hasTestenvMain(t, ctx, dir); got != tc.want {
				t.Errorf("hasTestenvMain = %v, want %v", got, tc.want)
			}
		})
	}
}

// Any package whose tests link code that reads home/config dirs or the OS
// keyring must isolate itself in a TestMain that calls testenv.Run, and
// testenv must never ship in the binary.
func TestPackagesTouchingHomeUseTestenv(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	risky := []string{modPath + "internal/config", modPath + "internal/keychain", modPath + "internal/tokensource",
		modPath + "internal/selfupdate", "github.com/zalando/go-keyring"}
	// Not ./...: stray module caches under dev/ break it.
	pkgs := []string{".", "./cmd/...", "./internal/..."}

	list := func(args ...string) []byte {
		cmd := exec.Command("go", append([]string{"list"}, args...)...)
		cmd.Dir = root
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list %v: %v", args, err)
		}
		return out
	}

	for _, dep := range strings.Fields(string(list("-deps", "-f", "{{.ImportPath}}", "."))) {
		if dep == modPath+"internal/testenv" {
			t.Errorf("internal/testenv is a dependency of the shipped binary")
		}
	}

	dec := json.NewDecoder(strings.NewReader(string(list(append([]string{"-test", "-json=ImportPath,Dir,Deps"}, pkgs...)...))))
	checked := 0
	for {
		var p struct {
			ImportPath, Dir string
			Deps            []string
		}
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		// The generated test main lists the full transitive deps of the tests.
		if !strings.HasSuffix(p.ImportPath, ".test") || !slices.ContainsFunc(risky, func(r string) bool { return slices.Contains(p.Deps, r) }) {
			continue
		}
		checked++
		if !hasTestenvMain(t, build.Default, p.Dir) {
			t.Errorf("%s: tests reach home/config/keyring code but no TestMain calls testenv.Run", p.ImportPath)
		}
	}
	if checked < 8 {
		t.Errorf("checked only %d packages; go list output not understood", checked)
	}
}

func TestClaimTestProcessRefusesAChild(t *testing.T) {
	env := map[string]string{}
	get := func(k string) string { return env[k] }
	set := func(k, v string) error { env[k] = v; return nil }
	if err := claimTestProcess(get, set, 42); err != nil || env[ParentEnv] != "42" {
		t.Fatalf("first claim: err=%v env=%v", err, env)
	}
	if err := claimTestProcess(get, set, 43); err == nil || !strings.Contains(err.Error(), ParentEnv) || env[ParentEnv] != "42" {
		t.Fatalf("second claim: err=%v env=%v, want a refusal that keeps the parent's marker", err, env)
	}
	if strings.HasPrefix(strings.ToUpper(ParentEnv), "C1I_") {
		t.Fatalf("%s would be stripped by Isolate", ParentEnv)
	}
}
