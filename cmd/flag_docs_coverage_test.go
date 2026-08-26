package cmd

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// This file pins the "a shipped flag is discoverable from a top-level doc"
// convention. The failure it exists for is not a typo — it is an agent
// rebuilding by hand something c1i already does, because neither top-level
// doc ever named the flag: a hand-rolled MCP registration that
// `mcp servers register --tool-prefix` already covered, and a hand-rolled
// pagination loop that `--paginate` had always covered.
//
// Both assertions resolve real flags from rootCmd's tree and real doc text.
// Nothing here calls RunE, so no test touches auth or the network.
//
// Scope, decided and settled: coverage is per flag NAME, not per (command,
// flag) pair — a flag documented on one command counts as documented on every
// command that registers it. Per-pair coverage was weighed and rejected: it
// would require a full per-command flag matrix in both docs, a maintenance
// burden larger than the problem it solves, and it is not the thing that
// failed in the field. What failed was a flag being findable from no doc at
// all, which this does catch. Treat the narrower property as the intended
// design, not as an unfinished one.

// flagDocExemptions lists flags deliberately absent from BOTH README.md and
// cmd/agents.md, each with the reason. Keep it short: an entry here is a
// promise that no caller ever needs to discover the flag from a doc. Prefer
// documenting the flag in one line over adding to this map.
var flagDocExemptions = map[string]string{
	// (empty) Every flag c1i registers is currently named in at least one of
	// the two top-level docs. Add an entry only for a flag no caller needs to
	// find, never to silence this guard for a flag that simply lacks a mention.
}

// globalFlagDocExemptions lists root persistent flags deliberately absent from
// cmd/agents.md specifically. Same rule: a reason per entry, and prefer a line
// of docs over an entry.
var globalFlagDocExemptions = map[string]string{}

// docMentionsFlag reports whether doc names the long flag --name. The trailing
// boundary matters: without it "--wait" would be satisfied by a doc that only
// ever mentions "--wait-timeout", which is how a genuinely undocumented flag
// hides behind a longer sibling.
func docMentionsFlag(doc, name string) bool {
	return regexp.MustCompile(`--` + regexp.QuoteMeta(name) + `([^\w-]|$)`).MatchString(doc)
}

// registeredFlags maps every long flag name reachable in rootCmd's tree to the
// command paths that register it. LocalFlags (not Flags) so a persistent flag
// inherited from an ancestor is attributed to the ancestor that declares it
// rather than to all ~90 commands beneath it. "help" is excluded: cobra adds it
// itself, and it is not part of c1i's documented surface.
//
// The three Init* calls matter for determinism, not completeness. cobra creates
// the "completion" subtree and the --version/--help flags lazily inside
// Execute(), so whether this walk sees "completion --no-descriptions" would
// otherwise depend on whether some earlier test in the package had already run
// a command — a guard that passes or fails by test ordering. Forcing them here
// makes the inventory the same on every run. All three are idempotent.
func registeredFlags(root *cobra.Command) map[string][]string {
	root.InitDefaultCompletionCmd()
	root.InitDefaultHelpCmd()
	root.InitDefaultVersionFlag()

	out := map[string][]string{}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if f.Name == "help" {
				return
			}
			out[f.Name] = append(out[f.Name], c.CommandPath())
		})
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
	return out
}

// summarizeOwners renders a flag's registering command paths for a failure
// message, capped so a flag on 27 list commands doesn't bury the message.
func summarizeOwners(paths []string) string {
	sort.Strings(paths)
	if len(paths) > 5 {
		return fmt.Sprintf("%s (+%d more)", strings.Join(paths[:5], ", "), len(paths)-5)
	}
	return strings.Join(paths, ", ")
}

// TestEveryFlagIsDocumented is the coverage guard: every long flag registered
// anywhere under rootCmd must be named in README.md or cmd/agents.md. README
// is the complete reference and agents.md the curated agent-facing index, so
// one of the two is the bar; which one is a judgment call per flag.
//
// cmd/agents.md is read through the same embedded agentsTemplate that
// "c1i docs agents" ships, so this checks the bytes a user actually receives.
func TestEveryFlagIsDocumented(t *testing.T) {
	readme := readDocFile(t, "../README.md")
	agents := agentsTemplate

	flags := registeredFlags(rootCmd)
	if len(flags) < 50 {
		// The walk silently returning near-nothing would make this guard pass
		// vacuously forever. c1i registered 89 flags when this was written.
		t.Fatalf("only %d flags found under rootCmd; the tree walk regressed", len(flags))
	}

	names := make([]string, 0, len(flags))
	for n := range flags {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		if reason, exempt := flagDocExemptions[name]; exempt {
			if docMentionsFlag(readme, name) || docMentionsFlag(agents, name) {
				t.Errorf("--%s is exempted (%q) but IS documented; drop the exemption", name, reason)
			}
			continue
		}
		if docMentionsFlag(readme, name) || docMentionsFlag(agents, name) {
			continue
		}
		t.Errorf("--%s (registered on %s) is named in neither README.md nor cmd/agents.md; "+
			"document it in one of them, or add it to flagDocExemptions with a reason",
			name, summarizeOwners(flags[name]))
	}
}

// TestGlobalFlagsDocumentedInAgentsDoc holds the persistent flags to a higher
// bar than the rest: they apply to every command, so an agent that reads only
// cmd/agents.md must still learn them. README.md alone is not enough here —
// --error-format shipped and sat unmentioned in the agent-facing index, which
// is exactly the gap this closes.
func TestGlobalFlagsDocumentedInAgentsDoc(t *testing.T) {
	readme := readDocFile(t, "../README.md")
	agents := agentsTemplate

	var checked int
	rootCmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		checked++
		if reason, exempt := globalFlagDocExemptions[f.Name]; exempt {
			if docMentionsFlag(agents, f.Name) {
				t.Errorf("global --%s is exempted (%q) but IS in cmd/agents.md; drop the exemption", f.Name, reason)
			}
			return
		}
		if !docMentionsFlag(agents, f.Name) {
			t.Errorf("global --%s is not named in cmd/agents.md; every persistent flag belongs in the agent-facing index", f.Name)
		}
		if !docMentionsFlag(readme, f.Name) {
			t.Errorf("global --%s is not named in README.md; README is the complete reference", f.Name)
		}
	})
	if checked == 0 {
		t.Fatal("no persistent flags found on rootCmd; the guard would pass vacuously")
	}
}
