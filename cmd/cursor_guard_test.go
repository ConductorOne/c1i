package cmd

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
)

// cursorGuardExemptions lists functions ("file:func") that read a pagination
// cursor without following it, so they need no paginationCursorGuard.
var cursorGuardExemptions = map[string]string{
	"api.go:extractListAndToken": "parser: returns the cursor to a guarded caller",
	"api.go:nextPageTokenOf":     "parser: feeds the non-paginate partial-result warning",
	"findings.go:getFixedList":   "fetches one page and warns if the server reports more; never follows the cursor",
}

// cursorReadFuncs are calls that surface a next-page cursor from a response.
var cursorReadFuncs = map[string]bool{
	"extractListAndToken":  true,
	"nextPageTokenOf":      true,
	"nextPageTokenFromMap": true,
}

// TestEveryCursorLoopIsGuarded fails when a non-test function in cmd/ reads a
// next-page cursor without the full paginationCursorGuard protocol, so a new
// list loop can't again spin forever on a cursor that stops advancing.
func TestEveryCursorLoopIsGuarded(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("globbing cmd/*.go: %v", err)
	}
	type scope struct {
		key                         string
		line                        int
		reads, guard, check, marked bool
	}
	var scopes []*scope
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		for _, decl := range file.Decls {
			name := "?"
			switch d := decl.(type) {
			case *ast.FuncDecl:
				name = d.Name.Name
			case *ast.GenDecl:
				if len(d.Specs) > 0 {
					if vs, ok := d.Specs[0].(*ast.ValueSpec); ok && len(vs.Names) > 0 {
						name = vs.Names[0].Name
					}
				}
			}
			// Each function (declared or literal) is its own scope; a nested
			// literal is not credited with its parent's guard, or vice versa.
			ast.Inspect(decl, func(n ast.Node) bool {
				var body *ast.BlockStmt
				switch f := n.(type) {
				case *ast.FuncDecl:
					body = f.Body
				case *ast.FuncLit:
					body = f.Body
				}
				if body == nil {
					return true
				}
				s := &scope{key: path + ":" + name, line: fset.Position(n.Pos()).Line}
				scopes = append(scopes, s)
				ast.Inspect(body, func(m ast.Node) bool {
					switch m := m.(type) {
					case *ast.FuncLit:
						return false
					case *ast.SelectorExpr:
						if m.Sel.Name == "NextPageToken" || m.Sel.Name == "NextCursor" {
							s.reads = true
						}
					case *ast.CallExpr:
						switch fn := m.Fun.(type) {
						case *ast.Ident:
							s.reads = s.reads || cursorReadFuncs[fn.Name]
							s.guard = s.guard || fn.Name == "newPaginationCursorGuard" || fn.Name == "newUncappedPaginationCursorGuard"
						case *ast.SelectorExpr:
							s.check = s.check || fn.Sel.Name == "tokenAlreadyUsed"
							s.marked = s.marked || fn.Sel.Name == "markTokenUsed"
						}
					}
					return true
				})
				return true
			})
		}
	}

	var reads int
	for _, s := range scopes {
		if !s.reads {
			continue
		}
		reads++
		if _, ok := cursorGuardExemptions[s.key]; ok {
			continue
		}
		if !s.guard || !s.check || !s.marked {
			t.Errorf("%s (line %d) reads a next-page cursor without newPaginationCursorGuard + tokenAlreadyUsed + markTokenUsed (guard=%v check=%v mark=%v); guard the loop or add a reasoned cursorGuardExemptions entry",
				s.key, s.line, s.guard, s.check, s.marked)
		}
	}
	if reads < 30 {
		t.Fatalf("found only %d functions reading a cursor; this scan is not looking at what it thinks it is", reads)
	}
	for key := range cursorGuardExemptions {
		found := false
		for _, s := range scopes {
			found = found || (s.key == key && s.reads)
		}
		if !found {
			t.Errorf("cursorGuardExemptions has stale entry %s: no such function reads a cursor", key)
		}
	}
}

func TestPaginationCursorGuardPageCap(t *testing.T) {
	g := newPaginationCursorGuard()
	g.maxPages = 3
	for i := 1; i <= 3; i++ {
		tok := fmt.Sprintf("tok-%d", i)
		if g.tokenAlreadyUsed(tok) {
			t.Fatalf("request %d stopped under a cap of 3", i)
		}
		g.markTokenUsed(tok)
	}
	if !g.tokenAlreadyUsed("tok-4") {
		t.Error("request 4 with a fresh token went through a cap of 3")
	}

	u := newUncappedPaginationCursorGuard()
	for i := 0; i < maxCursorPages+1; i++ {
		if u.tokenAlreadyUsed("") {
			t.Fatalf("uncapped guard stopped at request %d", i+1)
		}
	}
}

// An entitlement id repeats across apps, so a page of ids already seen under
// other apps is new data, not a stale page, and must be emitted.
func TestEntitlementListsKeyGuardOnAppAndID(t *testing.T) {
	var cases []listPaginationCase
	for _, tc := range listPaginationCases() {
		switch tc.name {
		case "entitlements list", "access-profiles requestable-entitlements list":
			cases = append(cases, tc)
		}
	}
	if len(cases) != 2 {
		t.Fatalf("found %d entitlement cases in listPaginationCases, want 2", len(cases))
	}
	page := func(app, next string) string {
		return fmt.Sprintf(`{"list":[{"appEntitlement":{"id":"ent1","appId":%q}}],"nextPageToken":%q}`, app, next)
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var requests int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if requests > 4 {
					t.Errorf("request %d; pagination did not terminate", requests)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch extractPageToken(t, c.method, r) {
				case "":
					_, _ = fmt.Fprint(w, page("app1", "tok1"))
				case "tok1":
					_, _ = fmt.Fprint(w, page("app2", "tok2"))
				default:
					_, _ = fmt.Fprint(w, page("app3", ""))
				}
			}))
			defer srv.Close()

			out, errOut, err := runListPaginationCase(t, c, srv)
			if err != nil {
				t.Fatalf("RunE: %v (stderr %q)", err, errOut)
			}
			var apps []string
			for _, row := range decodeNDJSONRows(t, out) {
				app, _ := row["app_id"].(string)
				apps = append(apps, app)
			}
			if strings.Join(apps, ",") != "app1,app2,app3" {
				t.Errorf("emitted rows for apps %v, want app1,app2,app3", apps)
			}
		})
	}
}

// Rows with no entitlement id carry no evidence of a repeat, so pages of them
// under fresh tokens must not trip the guard.
func TestEntitlementListsIgnoreRowsWithoutID(t *testing.T) {
	var cases []listPaginationCase
	for _, tc := range listPaginationCases() {
		switch tc.name {
		case "entitlements list", "access-profiles requestable-entitlements list":
			cases = append(cases, tc)
		}
	}
	if len(cases) != 2 {
		t.Fatalf("found %d entitlement cases in listPaginationCases, want 2", len(cases))
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var requests int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if requests > 4 {
					t.Errorf("request %d; pagination did not terminate", requests)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				next := map[string]string{"": "tok1", "tok1": "tok2"}[extractPageToken(t, c.method, r)]
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"list":[{}],"nextPageToken":%q}`, next)
			}))
			defer srv.Close()

			out, errOut, err := runListPaginationCase(t, c, srv)
			if err != nil {
				t.Fatalf("RunE: %v (stderr %q)", err, errOut)
			}
			if rows := decodeNDJSONRows(t, out); len(rows) != 3 || requests != 3 {
				t.Errorf("got %d rows over %d requests, want 3 over 3", len(rows), requests)
			}
		})
	}
}

// readRoles (auth login's scope menu) follows the role catalog's cursor and
// accumulates in memory, so a stuck cursor must end it with exit 6.
func TestReadRolesStuckCursorReturnsExitServer(t *testing.T) {
	for _, freshTokens := range []bool{false, true} {
		t.Run(fmt.Sprintf("freshTokens=%v", freshTokens), func(t *testing.T) {
			var roleRequests int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/v1/auth/introspect":
					_, _ = fmt.Fprint(w, `{"userId":"u-1"}`)
				case "/api/v1/users/u-1":
					_, _ = fmt.Fprint(w, `{"userView":{"user":{"roleIds":["r1"]}}}`)
				case "/api/v1/iam/roles":
					roleRequests++
					if roleRequests > 4 {
						t.Errorf("roles request %d; the stuck cursor was not caught", roleRequests)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					next := "tok-same"
					if freshTokens {
						next = fmt.Sprintf("tok-v%d", roleRequests)
					}
					_, _ = fmt.Fprintf(w, `{"list":[{"id":"r1","name":"system:user"}],"nextPageToken":%q}`, next)
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer srv.Close()

			_, err := readRoles(context.Background(), client.NewForTesting(srv.URL, srv.Client()))
			if err == nil {
				t.Fatal("readRoles returned nil error on a stuck cursor")
			}
			if got := exitCode(err); got != exitServer {
				t.Errorf("exitCode = %d, want exitServer (%d); err: %v", got, exitServer, err)
			}
			if !strings.Contains(err.Error(), "GET /api/v1/iam/roles") {
				t.Errorf("err = %q, want it to name the route", err)
			}
			want := 2
			if freshTokens {
				want = 3
			}
			if roleRequests != want {
				t.Errorf("roles requests = %d, want %d", roleRequests, want)
			}
		})
	}
}
