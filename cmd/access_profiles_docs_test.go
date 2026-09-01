package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// flatten collapses whitespace so a quoted error string still matches when a
// source wraps it across lines.
func flatten(s string) string { return strings.Join(strings.Fields(s), " ") }

// orderingQuote is the 400 whose unqualified restatement ("publish and it
// works") is the drift this guard exists to catch, so it is the one held to
// the qualifier. The other quote is the counter-example; a flag's own help
// scopes it already.
const orderingQuote = "catalog must be published to add an access entitlement"

// serverQuotes are the two 400s the docs promise, both claims about the server.
var serverQuotes = []string{
	orderingQuote,
	"catalog is visible to everyone, cannot add access entitlements",
}

// visibilityBindingSources are the docs stating when a visibility binding is
// accepted. The unqualified form keeps coming back, because it reads as true
// until you remember --visible-to-everyone is also a create-time flag.
var visibilityBindingSources = []string{
	"../README.md",
	"../CHANGELOG.md",
	"agents.md",
}

// qualifierWindow is how far from the quote the qualifier must sit. Matching
// anywhere in the file would let an append-only CHANGELOG satisfy a new
// release's unqualified entry with an older entry's wording.
const qualifierWindow = 600

// negations satisfy a substring check for the qualifier while asserting its
// opposite.
var negations = []string{
	"whether or not visible to everyone",
	"whether or not it is visible to everyone",
	"regardless of visible-to-everyone",
}

// TestVisibilityBindingClaimStaysQualified holds each source, and the create
// help, to the qualified form. Publishing is necessary but not sufficient: a
// profile created --published AND --visible-to-everyone is still refused.
func TestVisibilityBindingClaimStaysQualified(t *testing.T) {
	if len(visibilityBindingSources) == 0 || len(serverQuotes) == 0 {
		t.Fatal("no sources or no quotes to check — this guard would prove nothing")
	}

	for _, path := range visibilityBindingSources {
		checkQualified(t, path, flatten(readDocFile(t, path)))
	}
	checkQualified(t, "access-profiles create help",
		flatten(accessProfilesCreateCmd.Long+" "+flagUsages(accessProfilesCreateCmd)))
}

// checkQualified requires both server quotes to be present, the ordering one to
// carry the qualifier nearby, and no phrasing that negates it.
func checkQualified(t *testing.T, path, body string) {
	t.Helper()
	lower := strings.ToLower(body)

	for _, n := range negations {
		if strings.Contains(lower, n) {
			t.Errorf("%s says %q, which asserts the opposite: a profile created --published "+
				"AND --visible-to-everyone is refused", path, n)
		}
	}
	for _, quote := range serverQuotes {
		i := strings.Index(body, quote)
		if i < 0 {
			t.Errorf("%s no longer quotes %q", path, quote)
			continue
		}
		if quote == orderingQuote && !qualifiedNear(lower, i, len(quote)) {
			t.Errorf("%s quotes %q with no published-but-not-visible-to-everyone qualifier "+
				"within %d characters", path, quote, qualifierWindow)
		}
	}
}

// qualifiedNear reports whether the qualifier sits within qualifierWindow
// characters either side of the quote at index i.
func qualifiedNear(lower string, i, quoteLen int) bool {
	start := max(0, i-qualifierWindow)
	end := min(len(lower), i+quoteLen+qualifierWindow)
	near := lower[start:end]
	for _, phrase := range []string{
		"not visible to everyone",
		"not visible-to-everyone",
		"not `--visible-to-everyone`",
	} {
		if strings.Contains(near, phrase) {
			return true
		}
	}
	return false
}

// flagUsages concatenates a command's flag descriptions. One server string this
// guard checks is quoted in a flag's help, not in Long.
func flagUsages(cmd *cobra.Command) string {
	var b strings.Builder
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		b.WriteString(f.Usage)
		b.WriteString(" ")
	})
	return b.String()
}
