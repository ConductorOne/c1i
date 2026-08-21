package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// This file guards two enumerations that are duplicated across several
// documents and have drifted repeatedly because an edit to one copy misses
// the others: the exit-code taxonomy (every exit* constant declared anywhere
// in package cmd, restated in README.md, cmd/agents.md,
// .claude/commands/c1i.md, and CLAUDE.md) and the typed-error list (every
// concrete error type an errors.As check inside exitCode or
// classifyGatewayError resolves to, restated in CLAUDE.md and
// .claude/commands/c1i.md).
//
// Both guards discover their "source of truth" set by parsing the actual Go
// source with go/parser rather than hardcoding the current constant/type
// names here — a hardcoded list would need a matching manual update the
// moment a new exit code or classified type is added, which is exactly the
// silent-skip failure mode this file exists to prevent (a new exit 9 left
// out of every doc would also be left out of a hardcoded list, and the test
// would keep passing). Parsing the source means a new constant or a new
// errors.As check is picked up automatically and checked against the docs
// the next time this test runs.
//
// All four documents restate the exit-code taxonomy as a "| code | meaning |"
// markdown table, so Guard 1 uses one extraction path (extractTableBlock +
// codesInTable) for all of them — including CLAUDE.md, whose "Errors:"
// bullet used to carry this as prose. There is deliberately no prose parser
// here: a table cell can't be reworded into a sentence that hides its own
// leading code the way prose could.
//
// Every extraction step below either succeeds or calls t.Fatalf naming the
// document/section it could not parse — never a silent skip that would read
// as coverage it isn't providing.
//
// Known limits, left undetected on purpose (this is a guard test, not a
// general-purpose classifier): Guard 2 only sees classification done via
// errors.As inside exitCode or classifyGatewayError — a type assertion
// (err.(*T)), a type switch, or classification added in some other function
// is invisible to it. Guard 2 also dedupes discovered types by bare name
// (last dot-segment), so two same-named types in different packages would
// share one doc mention; no such collision exists today.

// exitConstant is one constant from cmd/errors.go's exit-code block.
type exitConstant struct {
	name  string
	value int
}

// parseExitConstants parses every non-test .go file in the cmd package
// directory and returns every top-level "exitXxx = N" integer constant
// declared anywhere in it, in file-glob-then-source order. Scanning the
// whole package rather than just errors.go is deliberate: an exit* constant
// declared in any other file in package cmd must still be caught by this
// guard, not silently invisible to it.
func parseExitConstants(t *testing.T) []exitConstant {
	t.Helper()

	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("globbing cmd/*.go: %v", err)
	}
	if len(paths) == 0 {
		t.Fatalf("found no .go files in the cmd package directory — has the test's working directory changed?")
	}

	var consts []exitConstant
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
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if !strings.HasPrefix(name.Name, "exit") {
						continue
					}
					if i >= len(vs.Values) {
						continue // no explicit value on this spec (e.g. iota continuation) — not used by this const block today
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.INT {
						t.Fatalf("%s: constant %s's value is not a plain integer literal (got %T) — parseExitConstants can't evaluate it; give it a literal int or update this parser", path, name.Name, vs.Values[i])
					}
					n, err := strconv.Atoi(lit.Value)
					if err != nil {
						t.Fatalf("%s: constant %s = %q did not parse as an int: %v", path, name.Name, lit.Value, err)
					}
					consts = append(consts, exitConstant{name: name.Name, value: n})
				}
			}
		}
	}
	if len(consts) == 0 {
		t.Fatalf("cmd/*.go: found no \"exitXxx\" constants — has the exit-code const block moved or been renamed? parseExitConstants can't discover the source of truth without it")
	}
	return consts
}

// tableRowCodeRe matches a markdown table row whose first cell is a bare or
// backtick-wrapped integer: "| 6 | ... |" or "| `6` | ... |", optionally
// indented (CLAUDE.md's table sits inside a list-item continuation, so its
// rows carry the item's leading spaces; the other three docs' tables don't).
// The docs deliberately phrase the rest of each row differently, so only the
// leading code cell is extracted.
var tableRowCodeRe = regexp.MustCompile("(?m)^[ \t]*\\|\\s*`?(\\d+)`?\\s*\\|")

// maxHeadingToTableLookahead bounds how many non-table lines extractTableBlock
// will scan past a heading before giving up — enough for a short intro
// paragraph, not so much that it could wander into an unrelated later table.
const maxHeadingToTableLookahead = 15

// extractTableBlock returns the contiguous run of "|"-prefixed lines making
// up the markdown table that follows the first line matching headingRe in
// content. Fails the test (never returns silently empty) if the heading or a
// table near it can't be found.
func extractTableBlock(t *testing.T, docPath string, content string, headingRe *regexp.Regexp) string {
	t.Helper()

	loc := headingRe.FindStringIndex(content)
	if loc == nil {
		t.Fatalf("%s: could not find a heading matching %q — has the exit-code section been renamed or removed?", docPath, headingRe.String())
	}

	lines := strings.Split(content[loc[1]:], "\n")
	var tableLines []string
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "|") {
			tableLines = append(tableLines, line)
			continue
		}
		if len(tableLines) > 0 {
			break // table ended
		}
		if i >= maxHeadingToTableLookahead {
			break
		}
	}
	if len(tableLines) == 0 {
		t.Fatalf("%s: found the heading matching %q but no markdown table (a line starting with \"|\") within %d lines after it", docPath, headingRe.String(), maxHeadingToTableLookahead)
	}
	return strings.Join(tableLines, "\n")
}

// codesInTable extracts the set of exit-code integers named in a markdown
// table block (see extractTableBlock). Fails the test if the table was
// located but zero row codes could be extracted from it — that means the row
// format itself has drifted from what tableRowCodeRe expects, which is a
// parse failure, not "zero documented codes."
func codesInTable(t *testing.T, docPath, tableBlock string) map[int]bool {
	t.Helper()

	found := map[int]bool{}
	for _, m := range tableRowCodeRe.FindAllStringSubmatch(tableBlock, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		found[n] = true
	}
	if len(found) == 0 {
		t.Fatalf("%s: located the exit-code table but extracted zero code numbers from it — row format no longer matches \"| N | ... |\" / \"| `N` | ... |\"?", docPath)
	}
	return found
}

// claudeErrorsHeadingRe locates CLAUDE.md's "- **Errors:**" bullet, whose
// table extractTableBlock finds the same way it finds the other three docs'
// headings: scan forward for the first run of "|"-prefixed lines.
var claudeErrorsHeadingRe = regexp.MustCompile(`(?m)^- \*\*Errors:\*\*`)

// readDocFile reads path (relative to the cmd package directory, i.e. one
// level under the module root) and fails the test loudly if it's missing —
// a missing document is exactly the kind of thing this guard must not treat
// as "nothing to check."
func readDocFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}

// TestExitCodeTaxonomyDocumentedEverywhere is Guard 1: every exit* constant
// declared anywhere in package cmd must appear, by its integer value, in
// README.md's, cmd/agents.md's, .claude/commands/c1i.md's, and CLAUDE.md's
// exit-code tables, AND every code any of those tables names must correspond
// to a real exit* constant — the source-to-docs direction alone would let a
// stale or fabricated row (e.g. a leftover "| 99 | ... |" for a code nothing
// declares anymore) sit in a table forever. It does not compare prose — the
// four tables deliberately phrase each row's meaning differently — only the
// leading code cell.
//
// cmd/agents.md is read through the same agentsTemplate the "c1i docs
// agents" command embeds and serves, so this test covers what actually ships
// in the binary, not a copy of the file on disk that could itself drift from
// what go:embed captured.
func TestExitCodeTaxonomyDocumentedEverywhere(t *testing.T) {
	consts := parseExitConstants(t)
	constValues := map[int]bool{}
	for _, c := range consts {
		constValues[c.value] = true
	}

	readmeTable := extractTableBlock(t, "README.md", readDocFile(t, "../README.md"), regexp.MustCompile(`(?m)^### Errors & exit codes$`))
	agentsTable := extractTableBlock(t, "cmd/agents.md (embedded)", agentsTemplate, regexp.MustCompile(`(?m)^## Exit codes$`))
	c1iTable := extractTableBlock(t, ".claude/commands/c1i.md", readDocFile(t, "../.claude/commands/c1i.md"), regexp.MustCompile(`(?m)^## Errors & exit codes$`))
	claudeTable := extractTableBlock(t, "CLAUDE.md", readDocFile(t, "../CLAUDE.md"), claudeErrorsHeadingRe)

	docs := []struct {
		name  string
		codes map[int]bool
	}{
		{"README.md", codesInTable(t, "README.md", readmeTable)},
		{"cmd/agents.md (embedded)", codesInTable(t, "cmd/agents.md (embedded)", agentsTable)},
		{".claude/commands/c1i.md", codesInTable(t, ".claude/commands/c1i.md", c1iTable)},
		{"CLAUDE.md", codesInTable(t, "CLAUDE.md", claudeTable)},
	}

	for _, c := range consts {
		for _, doc := range docs {
			if !doc.codes[c.value] {
				t.Errorf("%s: missing exit code %d (%s) — cmd/errors.go declares it but this document's exit-code list doesn't name it", doc.name, c.value, c.name)
			}
		}
	}

	// Reverse direction: a code a table names but no exitXxx constant
	// declares is stale or fabricated documentation, and would otherwise sit
	// there undetected indefinitely.
	for _, doc := range docs {
		codes := make([]int, 0, len(doc.codes))
		for code := range doc.codes {
			codes = append(codes, code)
		}
		sort.Ints(codes)
		for _, code := range codes {
			if !constValues[code] {
				t.Errorf("%s: documents exit code %d, but no exitXxx constant in cmd/*.go declares it — stale or fabricated row?", doc.name, code)
			}
		}
	}
}

// errorsAsType is one concrete error type an errors.As check resolves to,
// e.g. "client.APIError" or "usageError", plus the bare identifier docs are
// allowed to use in place of the qualified form ("APIError").
type errorsAsType struct {
	qualified string // as written in source, e.g. "client.APIError"
	bare      string // last dot-segment, e.g. "APIError"
}

// findFuncDecl returns the top-level function named name in file, or nil.
func findFuncDecl(file *ast.File, name string) *ast.FuncDecl {
	for _, d := range file.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == name {
			return fd
		}
	}
	return nil
}

// starExprTypeString formats a "*pkg.Type" or "*Type" expression as
// "pkg.Type" / "Type". Returns "" for any other shape.
func starExprTypeString(expr ast.Expr) string {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return ""
	}
	switch t := star.X.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		if pkg, ok := t.X.(*ast.Ident); ok {
			return pkg.Name + "." + t.Sel.Name
		}
	}
	return ""
}

// collectLocalPointerVarTypes walks body and returns every local
// "var x *T" (or "var x *pkg.T") declaration as name -> formatted type
// string. errors.As targets in this codebase are always declared this way
// immediately before the check, which is what makes resolving them from the
// AST reliable.
func collectLocalPointerVarTypes(body *ast.BlockStmt) map[string]string {
	types := map[string]string{}
	ast.Inspect(body, func(n ast.Node) bool {
		gd, ok := n.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			return true
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || vs.Type == nil {
				continue
			}
			ts := starExprTypeString(vs.Type)
			if ts == "" {
				continue
			}
			for _, nm := range vs.Names {
				types[nm.Name] = ts
			}
		}
		return true
	})
	return types
}

// collectErrorsAsVarNames returns, for every "errors.As(err, &name)" call
// found in body, the variable name passed as the second argument.
func collectErrorsAsVarNames(body *ast.BlockStmt) []string {
	var names []string
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "As" {
			return true
		}
		pkgIdent, ok := sel.X.(*ast.Ident)
		if !ok || pkgIdent.Name != "errors" {
			return true
		}
		if len(call.Args) != 2 {
			return true
		}
		unary, ok := call.Args[1].(*ast.UnaryExpr)
		if !ok || unary.Op != token.AND {
			return true
		}
		ident, ok := unary.X.(*ast.Ident)
		if !ok {
			return true
		}
		names = append(names, ident.Name)
		return true
	})
	return names
}

// collectErrorsAsTypes finds funcName in the parsed file at path and returns
// every concrete error type an errors.As call inside it resolves to, in
// source order, deduplicated. Fails the test loudly if the function can't be
// found, or if some errors.As target's type can't be resolved — either means
// this parser's assumptions about the code's shape no longer hold, which is
// exactly the "unparseable" case that must not pass silently.
func collectErrorsAsTypes(t *testing.T, path, funcName string) []errorsAsType {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	fd := findFuncDecl(file, funcName)
	if fd == nil || fd.Body == nil {
		t.Fatalf("%s: could not find func %s — has it been renamed or moved? Guard 2 can't discover the classified error types without it", path, funcName)
	}

	varTypes := collectLocalPointerVarTypes(fd.Body)
	varNames := collectErrorsAsVarNames(fd.Body)
	if len(varNames) == 0 {
		t.Fatalf("%s: func %s contains no errors.As(err, &x) calls — has the classification logic moved elsewhere?", path, funcName)
	}

	seen := map[string]bool{}
	var out []errorsAsType
	for _, name := range varNames {
		qualified, ok := varTypes[name]
		if !ok {
			t.Fatalf("%s: func %s calls errors.As(err, &%s) but has no \"var %s *T\" declaration to resolve its type from — this parser's var-decl assumption no longer holds; update collectLocalPointerVarTypes", path, funcName, name, name)
		}
		if seen[qualified] {
			continue
		}
		seen[qualified] = true
		bare := qualified
		if i := strings.LastIndex(qualified, "."); i >= 0 {
			bare = qualified[i+1:]
		}
		out = append(out, errorsAsType{qualified: qualified, bare: bare})
	}
	return out
}

// TestTypedErrorListDocumentedEverywhere is Guard 2: every concrete error
// type that determines the process exit code must be named — by its bare
// identifier ("APIError", "TransportError", "usageError", ...), anywhere in
// the file, regardless of whether a document qualifies it with its package
// or which sentence carries it — in both CLAUDE.md and
// .claude/commands/c1i.md, the two documents that enumerate the typed-error
// list. Matching is deliberately whole-file rather than scoped to a
// specific heading/bullet: CLAUDE.md's prose around this list is expected to
// get reworded and rewrapped over time, and a guard tied to today's sentence
// structure would break on the next rewrite even though the docs still name
// every type. (This does mean a type mentioned elsewhere in a document for
// an unrelated reason would be missed as "still documented" if dropped from
// the typed-error list specifically — a precision/robustness tradeoff made
// deliberately in favor of surviving a rewrite.)
//
// The set is discovered from two functions, not one, because the
// classification of *mcpgateway.TransportError happens in two steps:
// classifyGatewayError (cmd/mcp_gateway.go) reclassifies it into an
// *upstreamError before cmd/errors.go's exitCode ever sees it — so exitCode
// alone would miss it even though both documents (correctly) describe it as
// part of the classified set.
func TestTypedErrorListDocumentedEverywhere(t *testing.T) {
	types := collectErrorsAsTypes(t, "errors.go", "exitCode")
	types = append(types, collectErrorsAsTypes(t, "mcp_gateway.go", "classifyGatewayError")...)

	seen := map[string]bool{}
	var deduped []errorsAsType
	for _, ty := range types {
		if seen[ty.bare] {
			continue
		}
		seen[ty.bare] = true
		deduped = append(deduped, ty)
	}

	docs := []struct {
		name string
		text string
	}{
		{"CLAUDE.md", readDocFile(t, "../CLAUDE.md")},
		{".claude/commands/c1i.md", readDocFile(t, "../.claude/commands/c1i.md")},
	}

	for _, ty := range deduped {
		bareRe := regexp.MustCompile(`\b` + regexp.QuoteMeta(ty.bare) + `\b`)
		for _, doc := range docs {
			if !bareRe.MatchString(doc.text) {
				t.Errorf("%s: missing typed error %s (classified as %s) — not named anywhere in this document", doc.name, ty.bare, ty.qualified)
			}
		}
	}
}
