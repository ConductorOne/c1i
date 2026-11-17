package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// paginationPrimitives are the pieces of a pagination loop. They live in
// cmd/paginate.go; a command that calls one has hand-rolled its own loop,
// which every future guard would then have to patch separately.
var paginationPrimitives = map[string]bool{
	"markTokenUsed":                    true,
	"tokenAlreadyUsed":                 true,
	"limitReached":                     true,
	"effectivePageSize":                true,
	"keepRows":                         true,
	"checkPage":                        true,
	"checkIDlessPage":                  true,
	"newPaginationCursorGuard":         true,
	"newUncappedPaginationCursorGuard": true,
	"withCompositeIDs":                 true,
	"warnStuck":                        true,
	"stuckError":                       true,
	"inStaleRun":                       true,
}

// paginationPrimitiveUses names every call of a primitive in file.
func paginationPrimitiveUses(fset *token.FileSet, file *ast.File) []string {
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := ""
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			name = fn.Name
		case *ast.SelectorExpr:
			name = fn.Sel.Name
		}
		if paginationPrimitives[name] {
			out = append(out, fset.Position(call.Pos()).String()+" "+name)
		}
		return true
	})
	return out
}

// TestNoPaginationLoopOutsideHelper fails when any file but cmd/paginate.go
// calls a pagination-loop primitive. Add list behavior to runPages and its
// adapters, not to a command.
func TestNoPaginationLoopOutsideHelper(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var scanned int
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") || path == "paginate.go" {
			continue
		}
		scanned++
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		for _, use := range paginationPrimitiveUses(fset, file) {
			t.Errorf("%s: pagination primitive outside cmd/paginate.go; express the list as a pageSpec for runPages", use)
		}
	}
	if scanned < 50 {
		t.Fatalf("scanned only %d files; this guard is not looking at what it thinks it is", scanned)
	}
}

// The guard must catch a hand-rolled loop in every spelling it can take.
func TestPaginationPrimitiveGuardCatchesPlantedLoop(t *testing.T) {
	const planted = `package cmd

func rolledByHand(guard *paginationCursorGuard, enc *emitter, limit int, token string) {
	for !limitReached(enc.Written(), limit) {
		if guard.tokenAlreadyUsed(token) {
			return
		}
		guard.markTokenUsed(token)
	}
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "planted.go", planted, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(paginationPrimitiveUses(fset, file), "\n")
	for _, want := range []string{"limitReached", "tokenAlreadyUsed", "markTokenUsed"} {
		if !strings.Contains(got, want) {
			t.Errorf("planted loop: %s not reported; got:\n%s", want, got)
		}
	}
}
