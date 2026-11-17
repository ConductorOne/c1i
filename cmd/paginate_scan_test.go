package cmd

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/ConductorOne/c1i/internal/transport"
)

// Every paged POST goes through postPageBody, which refuses a route the
// transport does not classify as a read; this checks the routes the list
// commands use, then the refusal itself.
func TestListCommandPostPathsAreReadOnly(t *testing.T) {
	paths := []string{
		client.Path("/api/v1/role-mining/suggestions/%s/users", "s1"),
		"/api/v1/search/role-mining/suggestions",
		spBindingsListPath,
	}
	for _, tc := range listPaginationCases() {
		if tc.method == http.MethodPost {
			paths = append(paths, tc.wantPath)
		}
	}
	for _, p := range paths {
		if !transport.ReadOnlyPOST(p) {
			t.Errorf("paged POST %s is not classified read-only by the transport", p)
		}
	}
	if len(paths) < 20 {
		t.Fatalf("checked only %d paths; the case table shrank", len(paths))
	}
}

func TestPagedPostRefusesNonReadPathBeforeSending(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer srv.Close()
	c := client.NewForTesting(srv.URL, srv.Client())
	fetch := postPage(context.Background(), "/api/v1/apps/x/delete", map[string]any{})
	_, err := fetch(c, 10, "")
	var werr *pagedWriteError
	if !errors.As(err, &werr) || !strings.Contains(err.Error(), "/api/v1/apps/x/delete") {
		t.Fatalf("err = %v, want a pagedWriteError naming the path", err)
	}
	if requests != 0 {
		t.Errorf("%d request(s) sent; a refused path must send none", requests)
	}
	if got := exitCode(err); got != 1 {
		t.Errorf("exit code = %d, want 1", got)
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
