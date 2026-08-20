package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// This file guards two enumerations that are duplicated across several
// documents and have drifted repeatedly because an edit to one copy misses
// the others: the exit-code taxonomy (cmd/errors.go's exit* constants,
// restated in README.md, cmd/agents.md, .claude/commands/c1i.md, and
// CLAUDE.md) and the typed-error list (every concrete error type that
// ultimately determines the exit code, restated in CLAUDE.md and
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
// Every extraction step below either succeeds or calls t.Fatalf naming the
// document/section it could not parse — never a silent skip that would read
// as coverage it isn't providing.

// exitConstant is one constant from cmd/errors.go's exit-code block.
type exitConstant struct {
	name  string
	value int
}

// parseExitConstants parses cmd/errors.go and returns every top-level
// "exitXxx = N" integer constant it declares, in source order. This is the
// source of truth for Guard 1 — not a copy of the names living here, which
// would go stale exactly like the docs it's meant to check.
func parseExitConstants(t *testing.T) []exitConstant {
	t.Helper()

	const path = "errors.go"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	var consts []exitConstant
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
	if len(consts) == 0 {
		t.Fatalf("%s: found no \"exitXxx\" constants — has the exit-code const block moved or been renamed? parseExitConstants can't discover the source of truth without it", path)
	}
	return consts
}

// tableRowCodeRe matches a markdown table row whose first cell is a bare or
// backtick-wrapped integer: "| 6 | ... |" or "| `6` | ... |". The docs
// deliberately phrase the rest of each row differently, so only the leading
// code cell is extracted.
var tableRowCodeRe = regexp.MustCompile("(?m)^\\|\\s*`?(\\d+)`?\\s*\\|")

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

// claudeErrorsBulletRe / claudeNextBulletRe bound CLAUDE.md's "- **Errors:**"
// bullet: from its own heading line to (but not including) the next
// top-level list item.
var (
	claudeErrorsBulletRe = regexp.MustCompile(`(?m)^- \*\*Errors:\*\*`)
	claudeNextBulletRe   = regexp.MustCompile(`(?m)^- `)
)

// extractClaudeErrorsBullet returns the full text of CLAUDE.md's "Errors:"
// bullet (heading line through its last continuation line), which is the one
// place in CLAUDE.md that restates both the exit-code taxonomy and the typed
// error list. Fails the test if the bullet, or its end boundary, can't be
// found.
func extractClaudeErrorsBullet(t *testing.T, content string) string {
	t.Helper()

	loc := claudeErrorsBulletRe.FindStringIndex(content)
	if loc == nil {
		t.Fatalf("CLAUDE.md: could not find the \"- **Errors:**\" bullet — has it been renamed or restructured?")
	}
	firstLineEnd := strings.IndexByte(content[loc[1]:], '\n')
	if firstLineEnd == -1 {
		t.Fatalf("CLAUDE.md: the \"- **Errors:**\" bullet has no content after its heading line")
	}
	searchFrom := loc[1] + firstLineEnd + 1
	endLoc := claudeNextBulletRe.FindStringIndex(content[searchFrom:])
	if endLoc == nil {
		t.Fatalf("CLAUDE.md: could not find the next top-level bullet after \"- **Errors:**\" to bound its extent")
	}
	return content[loc[0] : searchFrom+endLoc[0]]
}

// exitCodesAnchorRe locates the phrase "exit codes" (case-insensitive) that
// introduces the enumeration in CLAUDE.md's "Errors:" bullet. Extraction
// starts after this anchor rather than at the top of the bullet so that
// whatever punctuation follows it ("— 0 ok, ..." vs ": 0 ok, ...") doesn't
// matter — only the anchor phrase itself has to survive a rewording.
var exitCodesAnchorRe = regexp.MustCompile(`(?i)exit codes`)

// leadingIntRe matches a leading run of digits once a clause has had its
// surrounding punctuation/whitespace trimmed.
var leadingIntRe = regexp.MustCompile(`^\d+`)

// splitTopLevelClauses splits s on "," and ";" that are not nested inside
// parentheses, returning each clause (untrimmed). CLAUDE.md's enumeration
// nests per-code detail in parens ("2 usage (bad flags/args, an empty id, or
// API 400)"), so a plain comma split would fragment a single code's clause
// into several pieces at its own internal commas; only top-level separators
// should end a clause.
func splitTopLevelClauses(s string) []string {
	var clauses []string
	depth := 0
	start := 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',', ';':
			if depth == 0 {
				clauses = append(clauses, s[start:i])
				start = i + 1
			}
		}
	}
	clauses = append(clauses, s[start:])
	return clauses
}

// codesInClaudeProse extracts the set of exit-code integers named in a
// CLAUDE.md bullet's prose. It finds the "exit codes" anchor, splits the
// remainder into top-level clauses, and — for each clause that, once
// trimmed of quoting/punctuation, begins with a bare integer — records that
// integer. This depends only on the anchor phrase and comma-separated-list
// shape, not on any specific delimiter character or wording around each
// code, so a rewording of the surrounding sentence doesn't break it. Fails
// the test if the anchor or any codes can't be found — that means the
// prose no longer follows this list shape at all.
func codesInClaudeProse(t *testing.T, bullet string) map[int]bool {
	t.Helper()

	loc := exitCodesAnchorRe.FindStringIndex(bullet)
	if loc == nil {
		t.Fatalf("CLAUDE.md: located the \"Errors:\" bullet but found no %q anchor phrase in it — has the bullet stopped enumerating exit codes, or reworded past recognition?", "exit codes")
	}

	found := map[int]bool{}
	for _, clause := range splitTopLevelClauses(bullet[loc[1]:]) {
		c := strings.TrimSpace(clause)
		c = strings.TrimLeft(c, "`:—- \t\n")
		m := leadingIntRe.FindString(c)
		if m == "" {
			continue
		}
		n, err := strconv.Atoi(m)
		if err != nil {
			continue
		}
		found[n] = true
	}
	if len(found) == 0 {
		t.Fatalf("CLAUDE.md: found the \"exit codes\" anchor but extracted zero exit-code numbers after it — has the \"0 ok, 1 generic, ...\" comma-separated list shape changed?")
	}
	return found
}

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
// in cmd/errors.go must appear, by its integer value, in README.md's,
// cmd/agents.md's, .claude/commands/c1i.md's, and CLAUDE.md's exit-code
// enumerations. It does not compare prose — the four documents deliberately
// phrase each row differently — only the code number.
//
// cmd/agents.md is read through the same agentsTemplate the "c1i docs
// agents" command embeds and serves, so this test covers what actually ships
// in the binary, not a copy of the file on disk that could itself drift from
// what go:embed captured.
func TestExitCodeTaxonomyDocumentedEverywhere(t *testing.T) {
	consts := parseExitConstants(t)

	readmeTable := extractTableBlock(t, "README.md", readDocFile(t, "../README.md"), regexp.MustCompile(`(?m)^### Errors & exit codes$`))
	agentsTable := extractTableBlock(t, "cmd/agents.md (embedded)", agentsTemplate, regexp.MustCompile(`(?m)^## Exit codes$`))
	c1iTable := extractTableBlock(t, ".claude/commands/c1i.md", readDocFile(t, "../.claude/commands/c1i.md"), regexp.MustCompile(`(?m)^## Errors & exit codes$`))
	claudeBullet := extractClaudeErrorsBullet(t, readDocFile(t, "../CLAUDE.md"))

	docs := []struct {
		name  string
		codes map[int]bool
	}{
		{"README.md", codesInTable(t, "README.md", readmeTable)},
		{"cmd/agents.md (embedded)", codesInTable(t, "cmd/agents.md (embedded)", agentsTable)},
		{".claude/commands/c1i.md", codesInTable(t, ".claude/commands/c1i.md", c1iTable)},
		{"CLAUDE.md", codesInClaudeProse(t, claudeBullet)},
	}

	for _, c := range consts {
		for _, doc := range docs {
			if !doc.codes[c.value] {
				t.Errorf("%s: missing exit code %d (%s) — cmd/errors.go declares it but this document's exit-code list doesn't name it", doc.name, c.value, c.name)
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
