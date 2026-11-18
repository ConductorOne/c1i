package cmd

import (
	"go/ast"
	"regexp"
	"strings"
	"testing"
)

// fixedListHelpers send one request and do not auto-paginate.
var fixedListHelpers = map[string]bool{"getFixedList": true, "getFixedListKey": true}

// singleRequestParagraphs bounds the hand-written single-request list in each
// doc, so a command named elsewhere in the file cannot satisfy the check.
var singleRequestParagraphs = []struct{ file, start, end string }{
	{"../README.md", "except the single-request ones", "each of which warns"},
	{"agents.md", "The exceptions send one request.", "each print a stderr"},
}

// TestSingleRequestListsDocumented derives the getFixedList callers from the
// leaves' RunE source and requires each doc's single-request list to name
// exactly that set.
func TestSingleRequestListsDocumented(t *testing.T) {
	ix := loadSrcIndex(t, nil)
	want := map[string]bool{}
	for _, l := range leafCommands(t, ix) {
		if reachesFixedList(ix, l.root, map[ast.Node]bool{}) {
			want[strings.TrimPrefix(l.path, "c1i ")] = true
		}
	}
	if !want["bridges services"] || !want["findings routing-rules list"] {
		t.Fatalf("derived %v; bridges services or a factory-built findings list is missing, so the derivation is broken", keys(want))
	}
	for _, p := range singleRequestParagraphs {
		got := documentedSingleRequest(t, p.file, p.start, p.end)
		for _, c := range keys(want) {
			if !got[c] {
				t.Errorf("%s: single-request list omits %q, whose RunE calls getFixedList", p.file, c)
			}
		}
		for _, c := range keys(got) {
			if !want[c] {
				t.Errorf("%s: single-request list names %q, which does not call getFixedList", p.file, c)
			}
		}
	}
}

// reachesFixedList reports whether root calls a fixedListHelpers function,
// directly or through a package-level func, var or method it reaches (the
// same edges walkWith follows). Not followed: a fetch function passed in as a
// parameter; no command builds its RunE that way.
func reachesFixedList(ix *srcIndex, root ast.Node, seen map[ast.Node]bool) bool {
	if seen[root] {
		return false
	}
	seen[root] = true
	found := false
	ast.Inspect(root, func(n ast.Node) bool {
		if found {
			return false
		}
		switch x := n.(type) {
		case *ast.SelectorExpr:
			for _, m := range ix.methods[x.Sel.Name] {
				if reachesFixedList(ix, m, seen) {
					found = true
				}
			}
		case *ast.Ident:
			if fixedListHelpers[x.Name] {
				found = true
			} else if fd, ok := ix.funcs[x.Name]; ok && reachesFixedList(ix, fd, seen) {
				found = true
			} else if v, ok := ix.vars[x.Name]; ok && reachesFixedList(ix, v, seen) {
				found = true
			}
		}
		return !found
	})
	return found
}

var docCodeSpan = regexp.MustCompile("`([^`]+)`")

// documentedSingleRequest returns the commands named in backticks between
// start and end in file, with whitespace collapsed (markers and spans may
// wrap) and a|b groups expanded.
func documentedSingleRequest(t *testing.T, file, start, end string) map[string]bool {
	t.Helper()
	doc := strings.Join(strings.Fields(readDocFile(t, file)), " ")
	i := strings.Index(doc, start)
	if i < 0 {
		t.Fatalf("%s: single-request paragraph marker %q not found", file, start)
	}
	j := strings.Index(doc[i:], end)
	if j < 0 {
		t.Fatalf("%s: single-request paragraph end %q not found after %q", file, end, start)
	}
	out := map[string]bool{}
	for _, m := range docCodeSpan.FindAllStringSubmatch(doc[i:i+j], -1) {
		for _, c := range expandAlternatives(strings.Fields(m[1])) {
			out[c] = true
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s: no commands found in the single-request paragraph", file)
	}
	return out
}

// expandAlternatives turns ["a", "b|c", "d"] into ["a b d", "a c d"].
func expandAlternatives(words []string) []string {
	out := []string{""}
	for _, w := range words {
		var next []string
		for _, prefix := range out {
			for _, alt := range strings.Split(w, "|") {
				next = append(next, strings.TrimSpace(prefix+" "+alt))
			}
		}
		out = next
	}
	return out
}
