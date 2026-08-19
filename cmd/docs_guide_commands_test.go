package cmd

import (
	"regexp"
	"strings"
	"testing"
)

// This file guards against the class of bug that originally motivated it:
// the embedded "docs guide" runbooks are static strings with no compile-time
// link to the cobra commands they walk through, so a breaking CLI change
// (e.g. #50's flag-to-positional-arg migration) can leave a guide citing a
// command form that no longer exists — and nothing catches it, because
// TestGuideRegistryLookup et al. only check the guide bodies are non-empty.
//
// TestGuideCommandsResolveAgainstCobraTree extracts every "c1i ..." invocation
// from the guide bodies and resolves it against the real, live cobra command
// tree rooted at rootCmd (the same tree "c1i" itself dispatches through), then
// checks every "--flag" token used actually exists on the resolved command
// (its own flags, or persistent/global flags inherited from a parent). It
// deliberately never calls RunE, so it needs no auth and has no side effects.

// extractGuideInvocations returns every "c1i ..." invocation found in guide,
// covering three shapes actually used by the guide bodies:
//   - a command block: a line whose trimmed text starts with "c1i ", with
//     "\"-terminated lines joined to the next (shell line continuation).
//   - a command substitution: "VAR=$(c1i ... | other-command)" — only the
//     c1i invocation up to the closing ")" or a "|" is taken.
//   - a prose cross-reference in quotes: `"c1i sub cmd"`.
//
// It does not attempt a general-purpose shell parser; it is scoped to the
// patterns the guide bodies actually use today.
func extractGuideInvocations(t *testing.T, guide string) []string {
	t.Helper()

	var invocations []string

	// Command blocks, joining backslash line continuations first.
	rawLines := strings.Split(guide, "\n")
	var logical []string
	var cur strings.Builder
	for _, line := range rawLines {
		trimmedRight := strings.TrimRight(line, " \t")
		if strings.HasSuffix(trimmedRight, "\\") {
			cur.WriteString(strings.TrimSuffix(trimmedRight, "\\"))
			cur.WriteString(" ")
			continue
		}
		cur.WriteString(trimmedRight)
		logical = append(logical, strings.TrimSpace(cur.String()))
		cur.Reset()
	}
	if cur.Len() > 0 {
		logical = append(logical, strings.TrimSpace(cur.String()))
	}
	for _, line := range logical {
		if strings.HasPrefix(line, "c1i ") {
			invocations = append(invocations, line)
		}
	}

	// Command substitutions: $(c1i ... [| ...]) — stop at the first "|" or
	// ")" after the "c1i " that opened it.
	substRe := regexp.MustCompile(`\$\(\s*(c1i [^|)]*)`)
	for _, m := range substRe.FindAllStringSubmatch(guide, -1) {
		invocations = append(invocations, strings.TrimSpace(m[1]))
	}

	// Quoted prose cross-references: "c1i sub cmd".
	quotedRe := regexp.MustCompile(`"(c1i [^"\n]*)"`)
	for _, m := range quotedRe.FindAllStringSubmatch(guide, -1) {
		invocations = append(invocations, strings.TrimSpace(m[1]))
	}

	return invocations
}

// tokenizeInvocation splits an invocation into words on unquoted whitespace,
// stripping (not escaping) double quotes — good enough for the guides, which
// never nest or escape quotes. This is deliberately dumb about flag values:
// a quoted shell variable like "$TOOL_ID" or a placeholder like "<name>"
// becomes a plain token indistinguishable from any other positional word,
// which is fine because callers only ever look for tokens starting with
// "--".
func tokenizeInvocation(invocation string) []string {
	var tokens []string
	var cur strings.Builder
	inQuotes := false
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}
	for _, r := range invocation {
		switch {
		case r == '"':
			inQuotes = !inQuotes
		case r == ' ' && !inQuotes:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return tokens
}

// flagNameFromToken returns the long-flag name for a "--foo" or "--foo=bar"
// token, or "" if tok is not a long-flag token (a value, a shell variable, a
// placeholder, or a short flag — none of which the guides use for anything
// this test needs to check).
func flagNameFromToken(tok string) string {
	if !strings.HasPrefix(tok, "--") {
		return ""
	}
	name := strings.TrimPrefix(tok, "--")
	if i := strings.Index(name, "="); i >= 0 {
		name = name[:i]
	}
	return name
}

// validateInvocationsAgainstCobraTree is the shared drift-guard check: every
// invocation must resolve to a real, executable cobra command (walking
// rootCmd's actual tree — not a guess about naming), and every "--flag" it
// passes must actually be registered on that command, its own or inherited
// from a parent (persistent/global flags like --app-id on a scope command,
// or --url on rootCmd). Used against both the embedded "docs guide" runbooks
// and cmd/agents.md, so a drift in either is caught the same way.
func validateInvocationsAgainstCobraTree(t *testing.T, invocations []string) {
	t.Helper()

	for _, inv := range invocations {
		tokens := tokenizeInvocation(inv)
		if len(tokens) == 0 || tokens[0] != "c1i" {
			t.Errorf("invocation %q did not tokenize with a leading \"c1i\"", inv)
			continue
		}

		leaf, remaining, err := rootCmd.Find(tokens[1:])
		if err != nil {
			t.Errorf("invocation %q: rootCmd.Find failed: %v", inv, err)
			continue
		}
		if leaf.Run == nil && leaf.RunE == nil {
			// Find() stops at the deepest node it recognizes. A
			// group command (e.g. "mcp", or "mcp gateway" if that
			// existed) has no Run/RunE, so landing here means the
			// invocation's subcommand path doesn't fully resolve to
			// a real, executable command.
			t.Errorf("invocation %q resolved only to %q (a command group, not an executable leaf) — the subcommand path is wrong or no longer exists", inv, leaf.CommandPath())
			continue
		}

		// Force local+inherited (parent persistent/global) flags to
		// merge into leaf.Flags(), then check every --flag token
		// against that merged set.
		leaf.InheritedFlags()
		for _, tok := range remaining {
			flagName := flagNameFromToken(tok)
			if flagName == "" {
				continue // positional arg, shell variable, or placeholder — not a flag
			}
			if leaf.Flags().Lookup(flagName) == nil {
				t.Errorf("invocation %q: --%s is not a registered flag on %q (own or inherited)", inv, flagName, leaf.CommandPath())
			}
		}
	}
}

// TestGuideCommandsResolveAgainstCobraTree is the drift guard: every "c1i ..."
// invocation embedded in a guide must resolve against the real cobra tree
// (see validateInvocationsAgainstCobraTree).
//
// Regression check performed while writing this test (see the PR description
// / commit message for the exact steps): temporarily reverting one guide line
// to its old, pre-#50 flag form (e.g. "mcp tools approve --id \"$TOOL_ID\""
// instead of "mcp tools approve \"$TOOL_ID\"") makes this test fail with an
// "unregistered flag" error, then restoring the line makes it pass again —
// confirming the test actually detects the class of bug it's meant to catch.
func TestGuideCommandsResolveAgainstCobraTree(t *testing.T) {
	for name, guide := range docsGuides {
		guide := guide
		t.Run(name, func(t *testing.T) {
			invocations := extractGuideInvocations(t, guide)
			if len(invocations) == 0 {
				t.Fatalf("no \"c1i ...\" invocations found in guide %q; extraction regressed?", name)
			}
			validateInvocationsAgainstCobraTree(t, invocations)
		})
	}
}

// TestAgentsMDCommandsResolveAgainstCobraTree extends the same drift guard to
// cmd/agents.md — the embedded agent-facing bootstrap doc has no
// compile-time link to the cobra commands it names either, so without this
// it could drift exactly the way the guides could before
// TestGuideCommandsResolveAgainstCobraTree existed (and the way cmd/skill.md
// did, unvalidated, for its entire lifetime).
func TestAgentsMDCommandsResolveAgainstCobraTree(t *testing.T) {
	invocations := extractGuideInvocations(t, agentsTemplate)
	if len(invocations) == 0 {
		t.Fatalf("no \"c1i ...\" invocations found in cmd/agents.md; extraction regressed?")
	}
	validateInvocationsAgainstCobraTree(t, invocations)
}
