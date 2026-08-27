package cmd

import (
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

// transportFreeSenders reports the net/http sending constructs a file uses:
// a package-level sender above, or an http.Client composite literal (which
// carries none of internal/transport's guards, options, or user agent).
//
// Limitation, deliberate: this recognizes net/http. A package that builds its
// own http.RoundTripper and drives it directly, or that reaches for a
// third-party HTTP library, would slip past — accept that rather than chase
// every shape, and widen this set when one shows up.
func transportFreeSenders(path string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}

	httpName := ""
	for _, im := range f.Imports {
		if im.Path.Value != `"net/http"` {
			continue
		}
		httpName = "http"
		if im.Name != nil {
			httpName = im.Name.Name
		}
	}
	if httpName == "" {
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
	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := v.X.(*ast.Ident); ok && id.Name == httpName && httpSenderFuncs[v.Sel.Name] {
				add(httpName + "." + v.Sel.Name)
			}
		case *ast.CompositeLit:
			if se, ok := v.Type.(*ast.SelectorExpr); ok {
				if id, ok2 := se.X.(*ast.Ident); ok2 && id.Name == httpName && se.Sel.Name == "Client" {
					add(httpName + ".Client{}")
				}
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
var httpBypassFiles = map[string]string{
	"cmd/docs_search.go":  "docs search / docs page -> api.mintlify.com",
	"cmd/docs_openapi.go": "docs openapi / endpoints / endpoint -> conductorone.com",
	// internal/transport IS the shared transport; its http.Client is the one
	// every other package is supposed to get its guards and options from.
	"internal/transport/transport.go": "the shared transport itself, not a bypass",
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
			case ".git", "dev", "vendor":
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
