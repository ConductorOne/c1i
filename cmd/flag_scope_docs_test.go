package cmd

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Flag SCOPE coverage: --debug/--max-retries do not reach every command.
//
// The fetching `docs` subcommands call http.DefaultClient.Do directly instead
// of internal/transport, so both flags are inert there and no path/redirect
// guard applies. That single fact reached six documents, stated as an
// unqualified universal in four of them, before anyone checked it against the
// code — and the cost is a reader who debugs an empty `docs search` with
// --debug, sees no trace, and concludes no request was sent.
//
// The two tests below pin it from both ends: every doc that documents the flags
// must name the exception, and the exception must still be real. When the
// http.DefaultClient sites are fixed, the second test fails and sends whoever
// fixed them back to delete the carve-outs, so this cannot rot into a warning
// about a hazard that no longer exists.

// scopedFlags are the flags whose reach is not universal.
var scopedFlags = []string{"--debug", "--max-retries"}

// fetchingDocsSubcommands are the `docs` leaves that issue their own HTTP.
// "docs endpoints" and "docs endpoint" are distinct commands, matched with a
// trailing boundary so the longer name can't satisfy a mention of the shorter.
var fetchingDocsSubcommands = []string{
	"docs search", "docs page", "docs openapi", "docs endpoints", "docs endpoint",
}

// httpBypassFiles are the cmd/ files that reach the network without
// internal/transport. Keep in sync with the carve-outs; the second test below
// fails if reality drifts from this set in either direction.
var httpBypassFiles = map[string]string{
	"docs_search.go":  "docs search / docs page -> api.mintlify.com",
	"docs_openapi.go": "docs openapi / endpoints / endpoint -> conductorone.com",
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

// TestDocumentedFlagScopeExceptionIsStillReal ties the carve-outs to the code
// fact behind them. If the set of cmd/ files bypassing internal/transport
// changes in either direction, the docs are now wrong.
func TestDocumentedFlagScopeExceptionIsStillReal(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading cmd/: %v", err)
	}
	got := map[string]bool{}
	var scanned int
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		scanned++
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if strings.Contains(string(b), "http.DefaultClient") {
			got[name] = true
		}
	}
	if scanned < 50 {
		t.Fatalf("only %d non-test .go files scanned in cmd/; the walk regressed and this "+
			"guard would pass by reading nothing", scanned)
	}
	for name := range got {
		if _, known := httpBypassFiles[name]; !known {
			t.Errorf("cmd/%s newly bypasses internal/transport via http.DefaultClient: "+
				"--debug and --max-retries are inert there and no path/redirect guard applies. "+
				"Either build it on internal/transport, or widen the carve-out in README.md, "+
				"cmd/agents.md, CLAUDE.md and .claude/commands/c1i.md", name)
		}
	}
	for name, what := range httpBypassFiles {
		if !got[name] {
			t.Errorf("cmd/%s (%s) no longer uses http.DefaultClient — if it now goes through "+
				"internal/transport, the docs carve-outs naming it are stale: update README.md, "+
				"cmd/agents.md, CLAUDE.md and .claude/commands/c1i.md", name, what)
		}
	}
}
