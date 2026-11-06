package cmd

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
)

// stalePageItems renders stuckStaleRows JSON items from format (one %d), so
// two repeats of the page meet the guard's stale-row floor.
func stalePageItems(format string) string {
	items := make([]string, stuckStaleRows)
	for i := range items {
		items[i] = fmt.Sprintf(format, i)
	}
	return "[" + strings.Join(items, ",") + "]"
}

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
		key                                string
		line                               int
		reads, guard, check, marked, paged bool
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
							s.paged = s.paged || fn.Sel.Name == "checkPage" || fn.Sel.Name == "checkIDlessPage"
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
		if !s.guard || !s.check || !s.marked || !s.paged {
			t.Errorf("%s (line %d) reads a next-page cursor without newPaginationCursorGuard + tokenAlreadyUsed + checkPage or checkIDlessPage + markTokenUsed (guard=%v check=%v page=%v mark=%v); guard the loop or add a reasoned cursorGuardExemptions entry",
				s.key, s.line, s.guard, s.check, s.paged, s.marked)
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

// The cap counts pages since the last new id, not requests: a long healthy
// list at a tiny page size must never reach it.
func TestPaginationCursorGuardIdleCap(t *testing.T) {
	g := newPaginationCursorGuard()
	g.maxPages = 3
	for i := 1; i <= 10; i++ {
		tok := fmt.Sprintf("tok-%d", i)
		if g.tokenAlreadyUsed(tok) {
			t.Fatalf("request %d stopped although every page brought a new id", i)
		}
		if _, err := g.checkPage(nil, "GET /x", []string{tok}); err != nil {
			t.Fatalf("page %d reported stuck", i)
		}
		g.markTokenUsed(tok)
	}

	g = newPaginationCursorGuard()
	g.maxPages = 3
	for i := 1; i <= 3; i++ {
		if g.tokenAlreadyUsed(fmt.Sprintf("tok-%d", i)) {
			t.Fatalf("request %d stopped under a cap of 3 idle pages", i)
		}
		if err := g.checkIDlessPage(nil, "GET /x", 1); err != nil {
			t.Fatalf("id-less page %d: %v", i, err)
		}
	}
	if !g.tokenAlreadyUsed("tok-4") {
		t.Fatal("a 4th page with no new id went through a cap of 3")
	}
	if msg := g.stuckError("GET /x").Error(); !strings.Contains(msg, "no new rows in") {
		t.Errorf("cap error = %q, want it to say no new rows arrived", msg)
	}

	u := newUncappedPaginationCursorGuard()
	for i := 0; i < maxIdlePages+1; i++ {
		if u.tokenAlreadyUsed("") {
			t.Fatalf("uncapped guard stopped at request %d", i+1)
		}
	}
}

// A newest-first offset feed at --page-size 1 re-serves a row or two when
// rows are inserted between requests; that is not a stuck cursor until the
// stale run also covers stuckStaleRows rows.
func TestPaginationCursorGuardStaleRunNeedsRowFloor(t *testing.T) {
	g := newPaginationCursorGuard()
	_, _ = g.checkPage(nil, "GET /x", []string{"a"})
	for i := 0; i < stuckStaleRows-1; i++ {
		if keep, err := g.checkPage(nil, "GET /x", []string{"a"}); keep[0] || err != nil {
			t.Fatalf("stale page %d: keep=%v err=%v, want skipped but not stuck", i+1, keep, err)
		}
	}
	if _, err := g.checkPage(nil, "GET /x", []string{"a"}); exitCode(err) != exitServer {
		t.Errorf("stale run of %d rows over %d pages: err=%v, want a stuck-cursor exit 6", stuckStaleRows, stuckStaleRows, err)
	}
}

// A stuck server that adds an id-less row to every repeat of a seen page:
// only the id-less row is emitted, and the stale run still ends it well before
// the idle cap.
func TestPaginationCursorGuardStuckDespiteIDlessRow(t *testing.T) {
	g := newPaginationCursorGuard()
	g.maxPages = 50
	page := make([]string, stuckStaleRows)
	for i := range page {
		page[i] = fmt.Sprintf("r%d", i)
	}
	emitted := 0
	var err error
	for i := 0; err == nil && i < 1000; i++ {
		tok := fmt.Sprintf("tok-%d", i)
		if g.tokenAlreadyUsed(tok) {
			err = g.stuckError("GET /x")
			break
		}
		ids := page
		if i > 0 {
			ids = append(append([]string{}, page...), "")
		}
		var keep []bool
		if keep, err = g.checkPage(nil, "GET /x", ids); err == nil {
			emitted += len(keepRows(ids, keep))
		}
		g.markTokenUsed(tok)
	}
	if exitCode(err) != exitServer {
		t.Fatalf("err = %v (exit %d), want exit 6", err, exitCode(err))
	}
	if g.stopReason != "" {
		t.Errorf("stopped by %q, want the stale run to stop it before the cap", g.stopReason)
	}
	if want := stuckStaleRows + 1; emitted != want {
		t.Errorf("emitted %d rows, want %d (the page once, then only the id-less row)", emitted, want)
	}
}

// A run of empty pages under fresh tokens stops after maxEmptyPages; any
// page with rows resets the run.
func TestPaginationCursorGuardEmptyPageRun(t *testing.T) {
	for _, idless := range []bool{false, true} {
		g := newPaginationCursorGuard()
		check := func(rows int) error {
			if idless {
				return g.checkIDlessPage(nil, "GET /x", rows)
			}
			ids := make([]string, rows)
			for i := range ids {
				ids[i] = fmt.Sprintf("id-%d-%d", len(g.seenIDs), i)
			}
			_, err := g.checkPage(nil, "GET /x", ids)
			return err
		}
		for i := 0; i < 3*maxEmptyPages; i++ {
			rows := 0
			if i%(maxEmptyPages-1) == 0 {
				rows = 1
			}
			if err := check(rows); err != nil {
				t.Fatalf("idless=%v page %d: %v (empty runs never reached %d)", idless, i, err, maxEmptyPages)
			}
		}
		_ = check(1)
		var err error
		for i := 0; i < maxEmptyPages && err == nil; i++ {
			err = check(0)
		}
		if exitCode(err) != exitServer || !strings.Contains(err.Error(), "empty pages") {
			t.Errorf("idless=%v: %d empty pages in a row: err=%v, want exit 6 naming empty pages", idless, maxEmptyPages, err)
		}
	}
}

// A non-empty page where no row carries the id the command reads means the
// response shape changed; printing it would repeat rows with no check.
func TestPaginationCursorGuardRejectsPageWithNoIDs(t *testing.T) {
	g := newPaginationCursorGuard()
	_, err := g.checkPage(nil, "GET /x", []string{"", ""})
	var shapeErr *nonJSONResponseError
	if !errors.As(err, &shapeErr) || exitCode(err) != exitServer {
		t.Errorf("err = %v, want a *nonJSONResponseError (exit 6)", err)
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

// A newest-first offset feed at --page-size 1 that gains rows between
// requests re-serves the same row a few times. That is not a stuck cursor:
// the command must skip the repeats and finish with exit 0.
func TestTinyPageSizeOffsetFeedIsNotStuck(t *testing.T) {
	for _, name := range []string{"tasks list", "requests list"} {
		var tc listPaginationCase
		for _, c := range listPaginationCases() {
			if c.name == name {
				tc = c
			}
		}
		t.Run(name, func(t *testing.T) {
			feed := []string{"t3", "t2", "t1"}
			var requests int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if requests > 10 {
					t.Errorf("request %d; pagination did not terminate", requests)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				offset := 0
				if tok := extractPageToken(t, tc.method, r); tok != "" {
					offset, _ = strconv.Atoi(tok)
				}
				next := ""
				if offset+1 < len(feed) {
					next = strconv.Itoa(offset + 1)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, tc.page(feed[offset:offset+1], next))
				// Two inserts at the head, so the next two pages repeat t3.
				if requests <= 2 {
					feed = append([]string{fmt.Sprintf("new%d", requests)}, feed...)
				}
			}))
			defer srv.Close()

			tc.extraFlags = map[string]string{"page-size": "1"}
			if name == "requests list" {
				tc.extraFlags["all"] = "true"
			}
			out, errOut, err := runListPaginationCase(t, tc, srv)
			if err != nil {
				t.Fatalf("RunE: %v (exit %d); stderr=%q", err, exitCode(err), errOut)
			}
			if ids := tc.rowIDs(decodeNDJSONRows(t, out)); strings.Join(ids, ",") != "t3,t2,t1" {
				t.Errorf("rows = %v, want t3,t2,t1 (repeats skipped)", ids)
			}
		})
	}
}

// A finding audit's event id is unique only within its finding, so the same
// event id under another finding is a new row.
func TestFindingsAuditsKeyGuardOnFindingAndEvent(t *testing.T) {
	var tc listPaginationCase
	for _, c := range listPaginationCases() {
		if c.name == "findings audits search" {
			tc = c
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch extractPageToken(t, tc.method, r) {
		case "":
			_, _ = fmt.Fprint(w, `{"list":[{"findingId":"f1","eventId":"e1"}],"nextPageToken":"tok1"}`)
		case "tok1":
			_, _ = fmt.Fprint(w, `{"list":[{"findingId":"f2","eventId":"e1"}],"nextPageToken":"tok2"}`)
		default:
			_, _ = fmt.Fprint(w, `{"list":[{"findingId":"f3","eventId":"e1"}],"nextPageToken":""}`)
		}
	}))
	defer srv.Close()

	out, errOut, err := runListPaginationCase(t, tc, srv)
	if err != nil {
		t.Fatalf("RunE: %v (stderr %q)", err, errOut)
	}
	if rows := decodeNDJSONRows(t, out); len(rows) != 3 {
		t.Errorf("got %d rows, want 3 (one per finding)", len(rows))
	}
}

// readRoles (auth login's scope menu) follows the role catalog's cursor and
// accumulates in memory, so a stuck cursor must end it with exit 6.
func TestReadRolesStuckCursorReturnsExitServer(t *testing.T) {
	// Enough rows that two stale pages meet stuckStaleRows.
	var roles []string
	for i := 0; i < stuckStaleRows; i++ {
		roles = append(roles, fmt.Sprintf(`{"id":"r%d","name":"system:user"}`, i))
	}
	rolesPage := "[" + strings.Join(roles, ",") + "]"
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
					_, _ = fmt.Fprintf(w, `{"list":%s,"nextPageToken":%q}`, rolesPage, next)
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

// Every list command but the audit log, against a server that answers empty
// pages under fresh tokens: stop after maxEmptyPages with exit 6, not after
// the idle cap. The server ends the list soon after, so an unbounded loop
// fails here instead of hanging.
func TestListCommandsStopOnEmptyPageRun(t *testing.T) {
	for _, tc := range listPaginationCases() {
		if tc.name == "export events" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			var requests int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.Header().Set("Content-Type", "application/json")
				if requests > maxEmptyPages+10 {
					_, _ = fmt.Fprint(w, tc.page([]string{"last"}, ""))
					return
				}
				_, _ = fmt.Fprint(w, tc.page(nil, fmt.Sprintf("tok-%d", requests)))
			}))
			defer srv.Close()

			_, errOut, err := runListPaginationCase(t, tc, srv)
			if exitCode(err) != exitServer || requests != maxEmptyPages {
				t.Fatalf("exit %d after %d requests (err %v), want exit 6 after %d", exitCode(err), requests, err, maxEmptyPages)
			}
			if !strings.Contains(errOut, "empty pages") {
				t.Errorf("stderr = %q, want it to name the empty-page run", errOut)
			}
		})
	}
}

// Every list command with row ids, against a stuck server that adds an
// id-less row to each repeat of a seen page: the seen rows are not printed
// again, and the stale run ends it with exit 6.
func TestListCommandsSkipSeenRowsBesideIDlessRow(t *testing.T) {
	var seen []string
	for i := 0; i < stuckStaleRows; i++ {
		seen = append(seen, fmt.Sprintf("item-%d", i))
	}
	for _, tc := range listPaginationCases() {
		if tc.noRowID {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			var requests int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.Header().Set("Content-Type", "application/json")
				switch {
				case requests == 1:
					_, _ = fmt.Fprint(w, tc.page(seen, "tok-1"))
				case requests > 10:
					_, _ = fmt.Fprint(w, tc.page(nil, ""))
				default:
					_, _ = fmt.Fprint(w, tc.page(append(append([]string{}, seen...), ""), fmt.Sprintf("tok-%d", requests)))
				}
			}))
			defer srv.Close()

			out, _, err := runListPaginationCase(t, tc, srv)
			if exitCode(err) != exitServer || requests != 3 {
				t.Fatalf("exit %d after %d requests (err %v), want exit 6 after 3", exitCode(err), requests, err)
			}
			if rows := decodeNDJSONRows(t, out); len(rows) != len(seen)+1 {
				t.Errorf("got %d rows, want %d (the page once, then only the id-less row)", len(rows), len(seen)+1)
			}
		})
	}
}

// Every list command with row ids, against a response whose rows all lack the
// id it reads (a renamed or missing key): fail as an unrecognized shape
// rather than print rows the guard can't check.
// On a later page the earlier rows are already printed, so the error must
// also say the result is incomplete.
func TestListCommandsRejectRowsWithoutID(t *testing.T) {
	for _, tc := range listPaginationCases() {
		if tc.noRowID || tc.compositeID {
			continue
		}
		for _, badPage := range []int{1, 2} {
			t.Run(fmt.Sprintf("%s/page %d", tc.name, badPage), func(t *testing.T) {
				var requests int
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests++
					next := fmt.Sprintf("tok-%d", requests)
					if requests > 10 {
						next = ""
					}
					w.Header().Set("Content-Type", "application/json")
					if requests < badPage {
						_, _ = fmt.Fprint(w, tc.page([]string{"first"}, next))
						return
					}
					_, _ = fmt.Fprint(w, tc.page([]string{"", ""}, next))
				}))
				defer srv.Close()

				_, errOut, err := runListPaginationCase(t, tc, srv)
				var shapeErr *nonJSONResponseError
				if !errors.As(err, &shapeErr) || exitCode(err) != exitServer || requests != badPage {
					t.Fatalf("err %v (exit %d) after %d requests, want an unrecognized-shape exit 6 after %d", err, exitCode(err), requests, badPage)
				}
				incomplete := strings.Contains(err.Error(), "INCOMPLETE") && strings.Contains(errOut, "INCOMPLETE")
				if incomplete != (badPage > 1) {
					t.Errorf("err %q, stderr %q: INCOMPLETE reported = %v, want %v", err, errOut, incomplete, badPage > 1)
				}
			})
		}
	}
}

// Composite or expanded ids can legitimately be missing, so a page of rows
// without one is emitted, not rejected.
func TestCompositeIDListCommandsEmitRowsWithoutID(t *testing.T) {
	var n int
	for _, tc := range listPaginationCases() {
		if !tc.compositeID {
			continue
		}
		n++
		t.Run(tc.name, func(t *testing.T) {
			var requests int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				next := fmt.Sprintf("tok-%d", requests)
				if requests == 3 {
					next = ""
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, tc.page([]string{"", ""}, next))
			}))
			defer srv.Close()

			out, errOut, err := runListPaginationCase(t, tc, srv)
			if err != nil || requests != 3 {
				t.Fatalf("err %v after %d requests (stderr %q), want exit 0 after 3", err, requests, errOut)
			}
			if rows := decodeNDJSONRows(t, out); len(rows) != 6 {
				t.Errorf("got %d rows, want 6", len(rows))
			}
		})
	}
	if n != 5 {
		t.Fatalf("found %d compositeID cases, want 5", n)
	}
}

// A grant whose expanded account or entitlement is null still carries the
// binding's flat ids; they must identify the row and fill its output.
func TestGrantsListFallsBackToBindingIDs(t *testing.T) {
	var tc listPaginationCase
	for _, c := range listPaginationCases() {
		if c.name == "grants list" {
			tc = c
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next := "tok1"
		user := "u1"
		if extractPageToken(t, tc.method, r) == "tok1" {
			next, user = "", "u2"
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"list":[{"appEntitlementUserBinding":{"appId":"app1","appEntitlementId":"ent1","appUserId":%q,"appUser":null},"entitlement":null}],"nextPageToken":%q}`, user, next)
	}))
	defer srv.Close()

	out, errOut, err := runListPaginationCase(t, tc, srv)
	if err != nil {
		t.Fatalf("RunE: %v (stderr %q)", err, errOut)
	}
	rows := decodeNDJSONRows(t, out)
	if len(rows) != 2 || rows[0]["app_user_id"] != "u1" || rows[1]["app_user_id"] != "u2" || rows[0]["entitlement_id"] != "ent1" || rows[0]["app_id"] != "app1" {
		t.Errorf("rows = %v, want u1 and u2 with entitlement ent1 in app1", rows)
	}
	if id := grantGuardID(grantListItem{}); id != "" {
		t.Errorf("grantGuardID of an empty grant = %q, want empty", id)
	}
}

// The audit log can page through a long quiet stretch; its uncapped guard
// must not stop on empty pages.
func TestExportEventsIgnoresEmptyPageRun(t *testing.T) {
	var tc listPaginationCase
	for _, c := range listPaginationCases() {
		if c.name == "export events" {
			tc = c
		}
	}
	const pages = maxEmptyPages + 50
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if requests == pages {
			_, _ = fmt.Fprint(w, tc.page([]string{"last"}, ""))
			return
		}
		_, _ = fmt.Fprint(w, tc.page(nil, fmt.Sprintf("tok-%d", requests)))
	}))
	defer srv.Close()

	out, errOut, err := runListPaginationCase(t, tc, srv)
	if err != nil || requests != pages {
		t.Fatalf("err %v after %d requests (stderr %q), want exit 0 after %d", err, requests, errOut, pages)
	}
	if rows := decodeNDJSONRows(t, out); len(rows) != 1 {
		t.Errorf("got %d rows, want 1", len(rows))
	}
}

// An executions page whose items lack the automationExecution wrapper must
// fail, not print nothing at exit 0.
func TestAutomationsExecutionsListRejectsMissingWrapper(t *testing.T) {
	var tc listPaginationCase
	for _, c := range listPaginationCases() {
		if c.name == "automations executions list" {
			tc = c
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"list":[{"execution":{"id":"x1"}},{"execution":{"id":"x2"}}],"nextPageToken":""}`)
	}))
	defer srv.Close()

	out, _, err := runListPaginationCase(t, tc, srv)
	if exitCode(err) != exitServer {
		t.Fatalf("exit %d (err %v, stdout %q), want exit 6", exitCode(err), err, out)
	}
}

// Commands whose rows carry no id must not trip on a long run of non-empty
// pages, nor on empty pages that never run maxEmptyPages long.
func TestIDlessListCommandsFinishLongRuns(t *testing.T) {
	for _, tc := range listPaginationCases() {
		if !tc.noRowID {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			const pages = 2*maxEmptyPages + 5
			var requests int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				next := fmt.Sprintf("tok-%d", requests)
				if requests == pages {
					next = ""
				}
				var ids []string
				if requests%2 == 0 {
					ids = []string{fmt.Sprintf("row-%d", requests)}
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, tc.page(ids, next))
			}))
			defer srv.Close()

			out, _, err := runListPaginationCase(t, tc, srv)
			if err != nil || requests != pages {
				t.Fatalf("err %v after %d requests, want exit 0 after %d", err, requests, pages)
			}
			if rows := decodeNDJSONRows(t, out); len(rows) != pages/2 {
				t.Errorf("got %d rows, want %d", len(rows), pages/2)
			}
		})
	}
}
