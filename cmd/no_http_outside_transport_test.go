package cmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// No code outside internal/transport may send HTTP: that is what makes
// --debug, --max-retries and the path/redirect guards cover every command (see
// globalTransportOptions). The walk below fails on any file that does.

// httpSenderFuncs are net/http's package-level entry points that send a request
// on http.DefaultClient. Matching the *set* rather than one spelling matters:
// http.Get and friends are DefaultClient underneath and are the likelier
// accidental spelling, and a guard that pinned only "http.DefaultClient" would
// tell a reader who switched a bypassing file to &http.Client{} that the
// carve-outs had gone stale when they were still true.
var httpSenderFuncs = map[string]bool{
	"DefaultClient": true,
	"Get":           true,
	"Post":          true,
	"Head":          true,
	"PostForm":      true,
}

// transportFreeSenders reports the net/http constructs a file uses that send a
// request outside internal/transport: a package-level sender above, or an
// http.Client CONSTRUCTED in any of the idiomatic ways (composite literal,
// new(), a value var, a value struct field). Matching construction rather than
// every mention of the type keeps a `*http.Client` parameter — mcpgateway.New
// takes one — from reading as a bypass.
//
// What this does NOT catch, stated in full because understating it is the same
// failure this file guards against: a hand-rolled http.RoundTripper driven
// directly (http.DefaultTransport.RoundTrip included), httputil.ReverseProxy,
// a local alias OR defined type for http.Client (`type c = http.Client`,
// `type c http.Client`), and any HTTP library other than net/http, stdlib or
// third-party. A dot-import of net/http is not missed but not analyzed either
// — it returns an error rather than a quiet pass, since selectors cannot be
// resolved through one.
func transportFreeSenders(path string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}

	// Collect EVERY local name net/http is bound to, not one of them. Picking
	// a single name (first or last) does not resolve a duplicate import
	// (`"net/http"` plus `nh "net/http"`) — it only chooses which of the two
	// spellings goes invisible, and which one that is flips with import order.
	httpNames := map[string]bool{}
	for _, im := range f.Imports {
		if im.Path.Value != `"net/http"` {
			continue
		}
		name := "http"
		if im.Name != nil {
			name = im.Name.Name
		}
		if name == "." {
			return nil, fmt.Errorf("dot-imports net/http; this guard cannot resolve selectors " +
				"through a dot-import, so it cannot tell whether the file bypasses internal/transport")
		}
		httpNames[name] = true
	}
	if len(httpNames) == 0 {
		return nil, nil
	}

	seen := map[string]bool{}
	var hits []string
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			hits = append(hits, s)
		}
	}
	// isHTTPClientType reports whether expr names the http.Client type itself.
	// A StarExpr (*http.Client) is deliberately not unwrapped: a pointer names
	// a client, it does not make one.
	isHTTPClientType := func(expr ast.Expr) (string, bool) {
		se, ok := expr.(*ast.SelectorExpr)
		if !ok || se.Sel.Name != "Client" {
			return "", false
		}
		id, ok := se.X.(*ast.Ident)
		if !ok || !httpNames[id.Name] {
			return "", false
		}
		return id.Name, true
	}

	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := v.X.(*ast.Ident); ok && httpNames[id.Name] && httpSenderFuncs[v.Sel.Name] {
				add(id.Name + "." + v.Sel.Name)
			}
		case *ast.CompositeLit:
			if n, ok := isHTTPClientType(v.Type); ok {
				add(n + ".Client{}")
			}
		case *ast.CallExpr:
			if id, ok := v.Fun.(*ast.Ident); ok && id.Name == "new" && len(v.Args) == 1 {
				if n, ok2 := isHTTPClientType(v.Args[0]); ok2 {
					add("new(" + n + ".Client)")
				}
			}
		case *ast.ValueSpec:
			if n, ok := isHTTPClientType(v.Type); ok {
				add("var " + n + ".Client")
			}
		case *ast.Field:
			if n, ok := isHTTPClientType(v.Type); ok {
				add(n + ".Client field")
			}
		}
		return true
	})
	return hits, nil
}

// httpSenderFiles are the only files allowed to send HTTP: the shared transport
// itself, whose http.Client every other package inherits. A new sender appended
// to it passes silently, which is accepted: changing the transport is not the
// drift this guards.
var httpSenderFiles = map[string]bool{"internal/transport/transport.go": true}

// TestNoHTTPOutsideTransport fails on a file that sends HTTP without the shared
// transport. The walk covers the whole repo, not just cmd/: CLAUDE.md's
// "Adding a new client/subsystem package" section is about internal/, which is
// the likelier home for the next bypass.
func TestNoHTTPOutsideTransport(t *testing.T) {
	const repoRoot = ".."

	got := map[string][]string{}
	var scanned int
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			// .claude holds .claude/worktrees/, which this branch's first
			// commit gitignored precisely because `git worktree add` targets
			// land there. Walking in finds every nested checkout's copy of
			// this repo and reports transport.go as a bypass — a local-only
			// failure (CI clones fresh) landing on the `go test ./...`
			// CLAUDE.md mandates before pushing.
			case ".git", ".claude", "dev", "vendor":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		scanned++
		hits, perr := transportFreeSenders(path)
		if perr != nil {
			t.Errorf("parsing %s: %v", path, perr)
			return nil
		}
		if len(hits) > 0 {
			rel, rerr := filepath.Rel(repoRoot, path)
			if rerr != nil {
				rel = path
			}
			got[filepath.ToSlash(rel)] = hits
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", repoRoot, err)
	}
	if scanned < 100 {
		t.Fatalf("only %d non-test .go files scanned; the walk regressed and this guard "+
			"would pass by reading nothing", scanned)
	}

	for path, hits := range got {
		if !httpSenderFiles[path] {
			t.Errorf("%s sends HTTP outside internal/transport (%v): --debug and --max-retries "+
				"would be inert and no path/redirect guard would apply. Build it with "+
				"transport.New(base, globalTransportOptions()...)", path, hits)
		}
	}
	for path := range httpSenderFiles {
		if _, ok := got[path]; !ok {
			t.Errorf("%s no longer sends HTTP; the walk or httpSenderFiles is stale", path)
		}
	}
}
