package cmd

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
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

// rawControlChars reports each character needsEscape would escape on stderr,
// and each invalid UTF-8 byte (a lone 0x9b is CSI on an 8-bit terminal).
func rawControlChars(s string) []string {
	var bad []string
	for i, line := range strings.Split(s, "\n") {
		for j := 0; j < len(line); {
			r, size := utf8.DecodeRuneInString(line[j:])
			switch {
			case r == utf8.RuneError && size == 1:
				bad = append(bad, fmt.Sprintf("line %d: invalid byte %#x", i+1, line[j]))
			case needsEscape(r):
				bad = append(bad, fmt.Sprintf("line %d: %U", i+1, r))
			}
			j += size
		}
	}
	return bad
}

func TestRawControlCharsFindsBidiAndC1(t *testing.T) {
	for _, s := range []string{"a\u202eb", "a\u2066b", "a\x1bb", "a\u0085b", "a\rb", "a\x9bb"} {
		if len(rawControlChars(s)) == 0 {
			t.Errorf("%q: not reported", s)
		}
	}
	if bad := rawControlChars("a\tb\nc `\\u202e` \\x1b"); len(bad) > 0 {
		t.Errorf("escape text reported: %v", bad)
	}
}
