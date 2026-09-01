package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// This repo is public, so fixtures and documentation use placeholders rather
// than identifiers or figures copied from a live tenant. Placeholders are
// stable, self-describing, and cannot go stale; a copied id addresses nothing
// the reader owns, and a copied count is out of date the moment it is written.
//
// Documentation states what the API does. Where a measurement backs a claim,
// the claim belongs here and the measurement belongs in the working notes.

// c1i's own OAuth client id, needed by the device flow. A product constant, not
// tenant data.
const productClientID = "juQSPDsPrdMDpPpR6fGdeLLSs8g"

// A platform constant present on every tenant: the builtin
// "Access" entitlement's shared id, documented because its repetition across
// apps is expected rather than a data error.
const sharedAccessEntitlementID = "287oY0rG4UirjDNFEYguMBvxyim"

var (
	// Object ids are 27 chars of [a-zA-Z0-9], and only count as data when they
	// appear as a literal -- a bare token that shape is a Go identifier.
	// Placeholders here carry a hyphen (user-1111..., cat-2222...) and cannot match.
	realObjectID = regexp.MustCompile("[\"`']([a-zA-Z0-9]{27})[\"`']")
	// Naming the tenant an observation came from adds nothing a reader can use.
	tenantPhrase = regexp.MustCompile(`(?i)\b(lab|test|demo) tenant\b`)
	// Hostnames of real tenants.
	tenantHost = regexp.MustCompile(`(?i)\bleet\b|\bleet\.conductor\.one\b`)
)

func TestFixturesAndDocsUsePlaceholders(t *testing.T) {
	allowedID := map[string]bool{productClientID: true, sharedAccessEntitlementID: true}

	err := filepath.Walk("..", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		name := info.Name()
		if info.IsDir() {
			// dev/ is gitignored scratch; the rest are not ours to police.
			if name == ".git" || name == "dev" || name == "node_modules" || name == ".claude" {
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(name) {
		case ".go", ".md", ".yaml", ".yml", ".json":
		default:
			return nil
		}
		if name == "placeholder_identifiers_test.go" {
			return nil // this file names the allowlisted constants
		}
		b, err := os.ReadFile(path) // #nosec G304 -- walking the repo's own tree
		if err != nil {
			return nil
		}
		body := string(b)

		for _, m := range realObjectID.FindAllStringSubmatch(body, -1) {
			id := m[1]
			if allowedID[id] || !looksLikeObjectID(id) {
				continue
			}
			t.Errorf("%s: identifier %q looks copied from a live tenant. Use a placeholder, e.g. user-1111111111111111111111.", path, id)
		}
		if loc := tenantPhrase.FindString(body); loc != "" {
			t.Errorf("%s: refers to a specific tenant (%q). State the behavior, not where it was seen.", path, loc)
		}
		if loc := tenantHost.FindString(body); loc != "" {
			t.Errorf("%s: contains a tenant hostname (%q). Use example.conductor.one.", path, loc)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the repo: %v", err)
	}
}

// looksLikeObjectID separates C1 ids from 27-character Go identifiers and
// English words, which the length check alone cannot tell apart.
func looksLikeObjectID(s string) bool {
	if strings.Contains(s, "_") {
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
	return digits >= 2 && upper >= 2 && lower >= 2
}
