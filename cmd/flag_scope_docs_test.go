package cmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Flag SCOPE coverage: --debug/--max-retries do not reach every command.
//
// The fetching `docs` subcommands send HTTP without internal/transport, so both
// flags are inert there and no path/redirect guard applies. That single fact
// reached six documents, stated as an unqualified universal in four of them,
// before anyone checked it against the code — and the cost is a reader who
// debugs an empty `docs search` with --debug, sees no trace, and concludes no
// request was sent.
//
// The two tests below pin it from both ends: every doc that documents the flags
// must name the exception, and the exception must still be real. When the
// bypassing call sites are fixed, the second test fails and sends whoever fixed
// them back to delete the carve-outs, so this cannot rot into a warning about a
// hazard that no longer exists.

// scopedFlags are the flags whose reach is not universal.
var scopedFlags = []string{"--debug", "--max-retries"}

// fetchingDocsSubcommands are the `docs` leaves that issue their own HTTP.
// "docs endpoints" and "docs endpoint" are distinct commands, matched with a
// trailing boundary so the longer name can't satisfy a mention of the shorter.
var fetchingDocsSubcommands = []string{
	"docs search", "docs page", "docs openapi", "docs endpoints", "docs endpoint",
}

// flagScopeDocs are the documents that describe what --debug and
// --max-retries do. agentsTemplate is the embedded copy `c1i docs agents`
// ships, so this checks the bytes a user actually receives.
func flagScopeDocs(t *testing.T) []struct{ name, body string } {
	t.Helper()
	return []struct{ name, body string }{
		{"README.md", readDocFile(t, "../README.md")},
		{"cmd/agents.md", agentsTemplate},
		{"CLAUDE.md", readDocFile(t, "../CLAUDE.md")},
		{".claude/commands/c1i.md", readDocFile(t, "../.claude/commands/c1i.md")},
	}
}

// namesFetchingDocsCommands counts how many of the five fetching subcommands a
// block of text names.
func namesFetchingDocsCommands(block string) int {
	n := 0
	for _, c := range fetchingDocsSubcommands {
		if regexp.MustCompile(regexp.QuoteMeta(c) + `([^\w-]|$)`).MatchString(block) {
			n++
		}
	}
	return n
}

// exceptionMarkerRe matches the wording a carve-out uses to say "not here".
// Requiring one stops a paragraph that merely lists the flags and the docs
// commands near each other from counting as an exception.
var exceptionMarkerRe = regexp.MustCompile(`(?i)\b(not|never|inert|ignored|bypass|exception|only|don't|doesn't)\b`)

// hasFlagScopeCarveOut reports whether doc contains a blank-line-separated
// block naming both scoped flags and at least two fetching docs subcommands,
// and saying they do not apply.
func hasFlagScopeCarveOut(doc string) bool {
	for _, block := range strings.Split(doc, "\n\n") {
		if !strings.Contains(block, scopedFlags[0]) || !strings.Contains(block, scopedFlags[1]) {
			continue
		}
		if namesFetchingDocsCommands(block) >= 2 && exceptionMarkerRe.MatchString(block) {
			return true
		}
	}
	return false
}

// TestFlagScopeExceptionDocumented requires every document that mentions
// --debug or --max-retries to also carve out the fetching `docs` subcommands.
func TestFlagScopeExceptionDocumented(t *testing.T) {
	var stated int
	for _, d := range flagScopeDocs(t) {
		mentions := false
		for _, f := range scopedFlags {
			if strings.Contains(d.body, f) {
				mentions = true
			}
		}
		if !mentions {
			continue
		}
		stated++
		if !hasFlagScopeCarveOut(d.body) {
			t.Errorf("%s documents %v but never carves out the fetching docs subcommands; "+
				"add a paragraph naming both flags, at least two of %v, and saying they do not apply there",
				d.name, scopedFlags, fetchingDocsSubcommands)
		}
	}
	if stated < 4 {
		t.Fatalf("only %d of 4 documents mention %v; the doc list or the flag names regressed, "+
			"and this guard would pass by checking nothing", stated, scopedFlags)
	}
}

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
// directly (http.DefaultTransport.RoundTrip included), a local type alias for
// http.Client, and any third-party HTTP library. A dot-import of net/http is
// not missed but not analyzed either — it returns an error rather than a quiet
// pass, since selectors cannot be resolved through one.
func transportFreeSenders(path string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}

	// First net/http import wins. Without the break a duplicate import
	// (`"net/http"` plus `nh "net/http"`) would leave httpName set to the last
	// spelling and silently miss every use of the first.
	httpName := ""
	for _, im := range f.Imports {
		if im.Path.Value != `"net/http"` {
			continue
		}
		httpName = "http"
		if im.Name != nil {
			httpName = im.Name.Name
		}
		break
	}
	if httpName == "" {
		return nil, nil
	}
	if httpName == "." {
		return nil, fmt.Errorf("dot-imports net/http; this guard cannot resolve selectors " +
			"through a dot-import, so it cannot tell whether the file bypasses internal/transport")
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
	isHTTPClientType := func(expr ast.Expr) bool {
		se, ok := expr.(*ast.SelectorExpr)
		if !ok || se.Sel.Name != "Client" {
			return false
		}
		id, ok := se.X.(*ast.Ident)
		return ok && id.Name == httpName
	}

	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := v.X.(*ast.Ident); ok && id.Name == httpName && httpSenderFuncs[v.Sel.Name] {
				add(httpName + "." + v.Sel.Name)
			}
		case *ast.CompositeLit:
			if isHTTPClientType(v.Type) {
				add(httpName + ".Client{}")
			}
		case *ast.CallExpr:
			if id, ok := v.Fun.(*ast.Ident); ok && id.Name == "new" && len(v.Args) == 1 {
				if isHTTPClientType(v.Args[0]) {
					add("new(" + httpName + ".Client)")
				}
			}
		case *ast.ValueSpec:
			if isHTTPClientType(v.Type) {
				add("var " + httpName + ".Client")
			}
		case *ast.Field:
			if isHTTPClientType(v.Type) {
				add(httpName + ".Client field")
			}
		}
		return true
	})
	return hits, nil
}

// httpBypassFiles are the repo-relative files that send HTTP without
// internal/transport, each with what it reaches. Keep it in sync with the
// carve-outs in the four documents; the test below fails if reality drifts
// from this set in either direction.
//
// Trap for the next reader: entries mean two different things and the loops
// below treat them identically. The two cmd/ files ARE bypasses, documented as
// such in the four docs. internal/transport is the opposite — it is the shared
// transport, so its http.Client is the one every other package is supposed to
// inherit; it is listed only to keep it from reporting itself. A new sender
// appended to transport.go would therefore pass silently, which is accepted:
// that file IS the transport, and changing it is not the drift this guards.
var httpBypassFiles = map[string]string{
	"cmd/docs_search.go":              "bypass: docs search / docs page -> api.mintlify.com",
	"cmd/docs_openapi.go":             "bypass: docs openapi / endpoints / endpoint -> conductorone.com",
	"internal/transport/transport.go": "NOT a bypass: the shared transport itself",
}

// TestDocumentedFlagScopeExceptionIsStillReal ties the carve-outs to the code
// fact behind them. The walk covers the whole repo, not just cmd/: CLAUDE.md's
// "Adding a new client/subsystem package" section is about internal/, which is
// the likelier home for the next bypass.
func TestDocumentedFlagScopeExceptionIsStillReal(t *testing.T) {
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
		if _, known := httpBypassFiles[path]; !known {
			t.Errorf("%s sends HTTP outside internal/transport (%v): --debug and --max-retries "+
				"are inert there and no path/redirect guard applies. Either build it on "+
				"internal/transport, or add it to httpBypassFiles and widen the carve-out in "+
				"README.md, cmd/agents.md, CLAUDE.md and .claude/commands/c1i.md", path, hits)
		}
	}
	for path, what := range httpBypassFiles {
		if _, still := got[path]; !still {
			t.Errorf("%s (%s) no longer sends HTTP outside internal/transport — if it now goes "+
				"through the shared transport, drop it from httpBypassFiles and re-check the "+
				"carve-outs naming it in README.md, cmd/agents.md, CLAUDE.md and "+
				".claude/commands/c1i.md", path, what)
		}
	}
}
