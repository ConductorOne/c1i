package cmd

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestEmbeddedDocsHoldNoRawControlChars: an embedded doc is printed to a
// terminal by `docs readme|changelog|agents|guide`, so a raw control or bidi
// character there acts on the screen. Write the escape text instead.
func TestEmbeddedDocsHoldNoRawControlChars(t *testing.T) {
	docs := map[string]string{"cmd/agents.md": agentsTemplate}
	for _, f := range []string{"README.md", "CHANGELOG.md"} {
		b, err := os.ReadFile("../" + f)
		if err != nil {
			t.Fatal(err)
		}
		docs[f] = string(b)
	}
	for name, body := range docsGuides {
		docs["guide "+name] = body
	}
	for name, body := range docs {
		if bad := rawControlChars(body); len(bad) > 0 {
			t.Errorf("%s: raw control/bidi characters: %s", name, strings.Join(bad, "; "))
		}
	}
}

// rawControlChars reports each character needsEscape would escape on stderr.
func rawControlChars(s string) []string {
	var bad []string
	for i, line := range strings.Split(s, "\n") {
		for _, r := range line {
			if needsEscape(r) {
				bad = append(bad, fmt.Sprintf("line %d: %U", i+1, r))
			}
		}
	}
	return bad
}

func TestRawControlCharsFindsBidiAndC1(t *testing.T) {
	for _, s := range []string{"a\u202eb", "a\u2066b", "a\x1bb", "a\u0085b", "a\rb"} {
		if len(rawControlChars(s)) == 0 {
			t.Errorf("%q: not reported", s)
		}
	}
	if bad := rawControlChars("a\tb\nc `\\u202e` \\x1b"); len(bad) > 0 {
		t.Errorf("escape text reported: %v", bad)
	}
}
