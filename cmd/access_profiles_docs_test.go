package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// flatten collapses whitespace so a quoted error string still matches when a
// source wraps it across lines — the failure mode that hid two earlier guards.
func flatten(s string) string { return strings.Join(strings.Fields(s), " ") }

// visibilityBindingSources are the docs that state when a visibility
// binding is accepted. The claim drifted twice in one branch: each source is
// individually correct when written, and the unqualified form ("a profile
// created with --published accepts them") is the one that keeps coming back,
// because it reads as true until you remember --visible-to-everyone is also a
// create-time flag.
// serverQuotes are the two 400s the docs promise. Both are claims about the
// server, and both are prone to the same unqualified restatement.
var serverQuotes = []string{
	"catalog must be published to add an access entitlement",
	"catalog is visible to everyone, cannot add access entitlements",
}

var visibilityBindingSources = []string{
	"../README.md",
	"../CHANGELOG.md",
	"agents.md",
}

// TestVisibilityBindingClaimStaysQualified holds every source that mentions
// publishing and visibility bindings to the qualified form. Publishing is
// necessary but not sufficient: a profile created --published AND
// --visible-to-everyone still refuses bindings, with a different 400.
func TestVisibilityBindingClaimStaysQualified(t *testing.T) {

	for _, path := range visibilityBindingSources {
		body := flatten(readDocFile(t, path))
		for _, quote := range serverQuotes {
			if !strings.Contains(body, quote) {
				t.Errorf("%s no longer quotes %q; dropping the server string is the same "+
					"drift event as restating the claim wrongly, so it is not a reason to skip this source", path, quote)
			}
		}
		if !mentionsPublishedNotVisible(body) {
			t.Errorf("%s documents the visibility-binding ordering but never says "+
				"published-but-not-visible-to-everyone; an unqualified \"created with "+
				"--published accepts them\" is false for a profile created with both flags", path)
		}
	}

	help := flatten(accessProfilesCreateCmd.Long + " " + flagUsages(accessProfilesCreateCmd))
	for _, quote := range serverQuotes {
		if !strings.Contains(help, quote) {
			t.Errorf("access-profiles create help no longer quotes %q", quote)
		}
	}
	if !mentionsPublishedNotVisible(help) {
		t.Error("access-profiles create help states the ordering without the not-visible-to-everyone qualifier")
	}
}

// mentionsPublishedNotVisible reports whether the text qualifies "published"
// with "not visible to everyone" somewhere. Wording differs per source, so
// this matches the property rather than a sentence.
func mentionsPublishedNotVisible(body string) bool {
	lower := strings.ToLower(flatten(body))
	for _, phrase := range []string{
		"not visible to everyone",
		"not visible-to-everyone",
		"but not `--visible-to-everyone`",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// flagUsages concatenates a command's flag descriptions. Half the server
// strings this guard checks are quoted in a flag's help, not in Long.
func flagUsages(cmd *cobra.Command) string {
	var b strings.Builder
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		b.WriteString(f.Usage)
		b.WriteString(" ")
	})
	return b.String()
}
