package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/login"
)

// This repo is public, so fixtures and documentation use placeholders rather
// than identifiers or figures copied from a live tenant. Placeholders are
// stable, self-describing, and cannot go stale; a copied id addresses nothing
// the reader owns, and a copied count is out of date the moment it is written.
//
// Documentation states what the API does. Where a measurement backs a claim,
// the claim belongs here and the measurement belongs in the working notes.

// c1i's own OAuth client id, needed by the device flow. A product constant, not
// tenant data. Referenced rather than retyped so rotating it can't make the
// guard reject the product's own value.
const productClientID = login.C1iClientID

// The builtin "Access" entitlement's id: a constant compiled into the C1
// backend and stamped onto every app at creation, so it is the same value on
// every tenant rather than tenant data. Allowlisted because its repetition
// across apps is expected, not a data error.
const sharedAccessEntitlementID = "287oY0rG4UirjDNFEYguMBvxyim"

// objectIDLen is the length of a C1 object id.
const objectIDLen = 27

var (
	// A copied id is data wherever it sits -- a JSON value, a request path, a
	// comment, an unquoted YAML value -- so the whole body is scanned.
	// Placeholders carry a hyphen (user-1111..., cat-2222...), which breaks the
	// token and cannot match.
	idToken = regexp.MustCompile(`[a-zA-Z0-9]+`)
	// Naming the tenant an observation came from adds nothing a reader can use.
	tenantPhrase = regexp.MustCompile(`(?i)\b(lab|test|demo) tenant\b`)
	// The tenant name, bare or in a hostname.
	tenantHost = regexp.MustCompile(`(?i)\bleet\b`)
)

func TestFixturesAndDocsUsePlaceholders(t *testing.T) {
	allowedID := map[string]bool{productClientID: true, sharedAccessEntitlementID: true}

	scanned := 0
	for _, path := range trackedFiles(t) {
		switch filepath.Ext(path) {
		case ".go", ".md", ".yaml", ".yml", ".json":
		default:
			continue
		}
		if filepath.Base(path) == "placeholder_identifiers_test.go" {
			continue // this file names the allowlisted constants
		}
		b, err := os.ReadFile(path) // #nosec G304 -- reading the repo's own tracked files
		if err != nil {
			if !os.IsNotExist(err) { // a staged deletion is not a finding
				t.Errorf("reading %s: %v", path, err)
			}
			continue
		}
		scanned++
		body := string(b)

		for _, id := range idToken.FindAllString(body, -1) {
			if len(id) != objectIDLen || allowedID[id] || !looksLikeObjectID(id) {
				continue
			}
			t.Errorf("%s: identifier %q looks copied from a live tenant. Use a placeholder, e.g. user-1111111111111111111111.", path, id)
		}
		if loc := tenantPhrase.FindString(body); loc != "" {
			t.Errorf("%s: refers to a specific tenant (%q). State the behavior, not where it was seen.", path, loc)
		}
		if loc := tenantHost.FindString(body); loc != "" {
			t.Errorf("%s: names the tenant (%q). Use example.conductor.one, or a placeholder.", path, loc)
		}
	}

	// Passing over an empty set reads as coverage but proves nothing.
	if scanned == 0 {
		t.Fatal("scanned no files; the guard inspected nothing")
	}
	t.Logf("scanned %d tracked files", scanned)
}

// trackedFiles lists the repo's tracked files, relative to this package. Only
// tracked files publish, and only they are worth policing: dev/ and .claude/
// hold local scratch but each also tracks a file that ships, so skipping those
// directories wholesale left published text unscanned.
func trackedFiles(t *testing.T) []string {
	t.Helper()
	// Distinguish "no checkout to inspect" from "git is broken": skipping on any
	// git failure would silently downgrade this guard to a passing no-op.
	if _, err := os.Stat(filepath.Join("..", ".git")); err != nil {
		t.Skipf("not a git checkout (%v), so there is nothing to inspect", err)
	}
	out, err := exec.Command("git", "-C", "..", "ls-files", "-z").Output()
	if err != nil {
		t.Fatalf("git ls-files failed inside a git checkout: %v", err)
	}
	var paths []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			paths = append(paths, filepath.Join("..", p))
		}
	}
	return paths
}

// looksLikeObjectID separates C1 ids from 27-character Go identifiers and
// English words, which the length check alone cannot tell apart. Names of test
// functions are excluded: TestAPIEmpty200BodySucceeds is 27 chars and satisfies
// the character mix, and this repo backticks test names in docs.
func looksLikeObjectID(s string) bool {
	if isTestFuncName(s) {
		return false
	}
	var digits, upper, lower int
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r >= 'A' && r <= 'Z':
			upper++
		default:
			lower++
		}
	}
	// A C1 id mixes all three densely; a CamelCase identifier has no digits.
	// Measured: ~5% of real ids miss the digit floor, and lowering it starts
	// catching ordinary words -- a false negative here is cheaper.
	return digits >= 2 && upper >= 2 && lower >= 2
}

// isTestFuncName reports whether s looks like a Go testing entry point. Go
// requires only that the character after the prefix is not a lowercase letter,
// so match that rather than an uppercase letter specifically.
func isTestFuncName(s string) bool {
	for _, p := range []string{"Test", "Benchmark", "Example", "Fuzz"} {
		if !strings.HasPrefix(s, p) || len(s) == len(p) {
			continue
		}
		if c := s[len(p)]; c < 'a' || c > 'z' {
			return true
		}
	}
	return false
}
