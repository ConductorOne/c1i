package cmd

import (
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// flatten collapses whitespace so a phrase still matches when a source wraps it.
func flatten(s string) string { return strings.Join(strings.Fields(s), " ") }

// orderingQuote is the 400 whose unqualified restatement ("publish and it
// works") is the drift this guard catches.
const orderingQuote = "catalog must be published to add an access entitlement"

// visibleQuote is the second 400, and the counter-example to that restatement.
const visibleQuote = "catalog is visible to everyone, cannot add access entitlements"

// qualifier is required verbatim wherever orderingQuote appears. One wording
// across every source is the point: earlier rounds tried to recognise any
// English phrasing that meant the same thing, and each version was defeated by
// a rewording — "whether or not visible to everyone" contains "not visible to
// everyone", so a sentence asserting the opposite satisfied the check. A fixed
// clause has no such hole, and rewording simply fails the test.
const qualifier = "published but not visible to everyone"

// visibilityBindingSources are the docs stating when a visibility binding is
// accepted. Publishing is necessary but not sufficient.
var visibilityBindingSources = []string{
	"../README.md",
	"../CHANGELOG.md",
	"agents.md",
}

// blockSplit breaks a doc where a claim can start: a blank line, a list item
// in any of CommonMark's marker forms, or a table row. Blank lines alone are
// not enough — a list has none between its items, and neither does a table, so
// either would be one block a new claim could borrow a distant clause from.
var blockSplit = regexp.MustCompile(`\n\s*\n|\n\s*(?:[-*+]|\d+[.)])\s|\n\s*\|`)

// fencedBlock matches a fenced code block in either marker form. A transcript
// of the server's error is an example, not a claim, and cannot carry
// explanatory prose, so requiring the clause inside one would fail on correct
// docs. Indented code blocks are not exempt; this repo fences.
var fencedBlock = regexp.MustCompile("(?s)```.*?```|(?s)~~~.*?~~~")

func TestVisibilityBindingClaimStaysQualified(t *testing.T) {
	if len(visibilityBindingSources) == 0 {
		t.Fatal("no sources to check — this guard would prove nothing")
	}

	for _, path := range visibilityBindingSources {
		checkQualified(t, path, readDocFile(t, path))
	}
	checkQualified(t, "access-profiles create help",
		accessProfilesCreateCmd.Long+"\n\n"+flagUsages(accessProfilesCreateCmd))
}

// checkQualified requires both quotes somewhere in the source, and the
// qualifier in EVERY block that states the ordering — not just the first.
func checkQualified(t *testing.T, path, raw string) {
	t.Helper()

	// Quotes may appear anywhere, transcripts included; claims may not.
	whole := flatten(raw)
	for _, quote := range []string{orderingQuote, visibleQuote} {
		if !strings.Contains(whole, quote) {
			t.Errorf("%s no longer quotes %q", path, quote)
		}
	}

	stating := 0
	for _, block := range blockSplit.Split(fencedBlock.ReplaceAllString(raw, ""), -1) {
		flat := flatten(block)
		if !strings.Contains(flat, orderingQuote) {
			continue
		}
		stating++
		if !strings.Contains(strings.ToLower(flat), qualifier) {
			t.Errorf("%s states the ordering without the exact clause %q in the same block; "+
				"publishing alone is not sufficient. Block begins: %q", path, qualifier, excerpt(flat))
		}
	}
	if stating == 0 {
		t.Errorf("%s: no block states the ordering, so nothing was checked", path)
	}
}

// flagUsages concatenates a command's flag descriptions. One quote this guard
// checks is in a flag's help, not in Long.
func flagUsages(cmd *cobra.Command) string {
	var b strings.Builder
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		b.WriteString(f.Usage)
		b.WriteString("\n\n")
	})
	return b.String()
}

// excerpt trims a block to something short enough to name it in a failure.
func excerpt(flat string) string {
	if len(flat) > 90 {
		return flat[:90] + "…"
	}
	return flat
}
