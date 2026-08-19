package cmd

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestGuideCommandsResolveAgainstCobraTree extracts every "c1i ..."
// invocation from the embedded guide bodies and resolves it against the real
// rootCmd tree: every "--flag"/"-f" must be registered (own or inherited),
// and the leftover positional count must be one the command's Args
// validator accepts. It never calls RunE, so it needs no auth.

// substInvocationRe and quotedInvocationRe are shared with
// findUnclaimedFlaggedMentions, so they're package-level.
var (
	substInvocationRe  = regexp.MustCompile(`\$\(\s*(c1i [^|)]*)`)
	quotedInvocationRe = regexp.MustCompile(`"(c1i [^"\n]*)"`)
	// redirectRe matches a trailing shell redirect ("> file" / ">> file") so
	// extraction can drop it — it's not part of the invocation's argv.
	redirectRe = regexp.MustCompile(`\s>{1,2}\s`)
	bareC1iRe  = regexp.MustCompile(`\bc1i\b`)
	// flagShapedTokenRe matches a "--flag" token in free prose. It stops at
	// the first character that isn't part of a flag name, so trailing
	// punctuation ("'s", ",", ".") is never mistaken for part of it.
	flagShapedTokenRe = regexp.MustCompile(`--[A-Za-z][A-Za-z0-9-]*`)
	// shorthandTokenRe matches a whole "-x" prose token: one dash, one
	// letter, then anything that can't continue a flag name. Applied per
	// whitespace-separated field, so adjacent tokens ("-q -z") are all
	// reported; a single regex over the line would consume the space
	// delimiting them and miss every one after the first. Never matches
	// "--long-flag" or a hyphenated word like "well-known".
	shorthandTokenRe = regexp.MustCompile(`^-([A-Za-z])(?:[^A-Za-z0-9-].*)?$`)
)

// extractGuideInvocations returns every "c1i ..." invocation in guide, in
// three recognized shapes: a command block (a line trimmed-starting with
// "c1i ", backslash-continued lines joined, a trailing redirect dropped), a
// command substitution ($(c1i ... | ...), truncated at "|" or ")"), and a
// quoted cross-reference ("c1i sub cmd"). See findUnclaimedFlaggedMentions
// for invocations these three shapes miss.
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
			if loc := redirectRe.FindStringIndex(line); loc != nil {
				line = line[:loc[0]]
			}
			invocations = append(invocations, line)
		}
	}

	for _, m := range substInvocationRe.FindAllStringSubmatch(guide, -1) {
		invocations = append(invocations, strings.TrimSpace(m[1]))
	}

	for _, m := range quotedInvocationRe.FindAllStringSubmatch(guide, -1) {
		invocations = append(invocations, strings.TrimSpace(m[1]))
	}

	return invocations
}

// checkUnclaimedMentions flags a "c1i" mention that falls outside all three
// recognized invocation shapes (e.g. unquoted mid-sentence prose, or a line
// prefixed with shell logic) but names a "--flag"/"-f"-shaped token later on
// the same line — the pattern a drifted flag can hide behind in ordinary
// prose like "the register command's --tool-prefix flag...". Rather than
// flagging on shape alone (which false-positives on any correct sentence
// that names a real command and a real flag), it resolves the longest
// matching command path from the words following "c1i" — reusing
// rootCmd.Find, the same resolver the shape-based invocations use — and
// checks each flag-shaped token on the line against THAT command's
// registered flags (own + inherited, shorthands included). It reports a
// failure only when a flag is genuinely not registered.
//
// It deliberately does not flag every bare "c1i" mention: guide prose
// routinely says things like "c1i has no command for this" with no
// invocation or flag meant. Requiring a nearby flag-shaped token narrows
// this to the actual risk at the cost of missing a positional-only drift in
// free prose (no flag involved) — a documented, accepted gap.
func checkUnclaimedMentions(t *testing.T, name, guide string) {
	t.Helper()

	type span struct{ start, end int }
	var claimed []span
	for _, re := range []*regexp.Regexp{quotedInvocationRe, substInvocationRe} {
		for _, m := range re.FindAllStringIndex(guide, -1) {
			claimed = append(claimed, span{m[0], m[1]})
		}
	}
	isClaimed := func(idx int) bool {
		for _, s := range claimed {
			if idx >= s.start && idx < s.end {
				return true
			}
		}
		return false
	}

	for _, m := range bareC1iRe.FindAllStringIndex(guide, -1) {
		start := m[0]
		if isClaimed(start) {
			continue
		}
		lineStart := strings.LastIndexByte(guide[:start], '\n') + 1
		rest := guide[start:]
		lineEnd := strings.IndexByte(rest, '\n')
		if lineEnd == -1 {
			lineEnd = len(rest)
		}
		line := guide[lineStart : start+lineEnd]
		trimmedLine := strings.TrimSpace(line)
		if strings.HasPrefix(trimmedLine, "c1i ") {
			continue // command-block line; already extracted & checked
		}

		afterMention := guide[start+len("c1i") : start+lineEnd]
		longFlags := flagShapedTokenRe.FindAllString(afterMention, -1)
		var shortFlags []string
		for _, f := range strings.Fields(afterMention) {
			if sm := shorthandTokenRe.FindStringSubmatch(f); sm != nil {
				shortFlags = append(shortFlags, sm[1])
			}
		}
		if len(longFlags) == 0 && len(shortFlags) == 0 {
			continue // no flag-shaped token following on this line
		}

		leaf, _, err := rootCmd.Find(strings.Fields(afterMention))
		if err != nil {
			t.Errorf("guide %q: %q: rootCmd.Find failed while resolving the command path named here: %v", name, trimmedLine, err)
			continue
		}
		leaf.InheritedFlags() // merge inherited flags before lookups

		for _, tok := range longFlags {
			if leaf.Flags().Lookup(strings.TrimPrefix(tok, "--")) == nil {
				t.Errorf("guide %q: %q names %s, which is not a registered flag on %q (own or inherited)", name, trimmedLine, tok, leaf.CommandPath())
			}
		}
		for _, letter := range shortFlags {
			if leaf.Flags().ShorthandLookup(letter) == nil {
				t.Errorf("guide %q: %q names -%s, which is not a registered shorthand flag on %q (own or inherited)", name, trimmedLine, letter, leaf.CommandPath())
			}
		}
	}
}

// tokenizeInvocation splits on unquoted whitespace, stripping both double
// and single quotes. Each quoted span tracks its own opening quote character,
// so a single-quoted span may contain a literal double quote (and a space)
// without ending the token early — needed for values like
// --args '{"key": "--not-a-flag"}'. Placeholders and shell variables
// ("$TOOL_ID", "<name>") become plain tokens; callers only look for tokens
// starting with "-".
//
// An unquoted "#" at a token boundary starts a trailing comment that runs to
// end of line and is dropped — e.g. `c1i mcp servers list --app-id "$X"  #
// note`. Comment detection and quote tracking happen in one left-to-right
// scan, so a "#" inside a quoted value (--args '{"a":"#x"}') is data, not a
// comment, and a "'" inside a comment doesn't open a quote span.
//
// If a quote span is still open at end of line, the invocation cannot be
// tokenized unambiguously (e.g. a bare apostrophe used as punctuation, not
// quoting) and tokenizeInvocation returns an error instead of silently
// degrading into a partially-parsed token stream — a caller that ignored
// this and used the tokens anyway could glue an unrelated flag into the
// unterminated value and never see it as a separate token.
func tokenizeInvocation(invocation string) ([]string, error) {
	var tokens []string
	var cur strings.Builder
	var quote rune // 0 when not inside a quoted span, else the quote rune
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}
	for _, r := range invocation {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '#' && cur.Len() == 0:
			return tokens, nil // trailing comment at a token boundary
		case r == '"' || r == '\'':
			quote = r
		case r == ' ':
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c-quoted value in %q", quote, invocation)
	}
	flush()
	return tokens, nil
}

// flagNameFromToken returns the long-flag name for a "--foo"/"--foo=bar"
// token, or "" if tok isn't one.
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

// shorthandFromToken returns the single-character shorthand for a "-x" or
// "-xvalue" token, or ok=false otherwise.
func shorthandFromToken(tok string) (shorthand string, hasInlineValue bool, ok bool) {
	if !strings.HasPrefix(tok, "-") || strings.HasPrefix(tok, "--") || tok == "-" {
		return "", false, false
	}
	rest := tok[1:]
	return rest[:1], len(rest) > 1, true
}

// collectPositionals walks remaining (the tokens after Find() resolved
// leaf's subcommand path) and returns the real positional arguments,
// reporting via t.Errorf any "--flag"/"-f" token not registered on leaf
// (own or inherited; call leaf.InheritedFlags() first). A flag's inline
// value ("--foo=bar", "-fbar") consumes no extra token; a non-bool flag
// given space-separated consumes the next token as its value (matching
// pflag); a literal "--" sends the rest to positionals.
func collectPositionals(t *testing.T, inv string, leaf *cobra.Command, remaining []string) []string {
	t.Helper()

	var positionals []string

	i := 0
	for i < len(remaining) {
		tok := remaining[i]
		switch {
		case tok == "--":
			i++
			positionals = append(positionals, remaining[i:]...)
			return positionals

		case strings.HasPrefix(tok, "--"):
			name := flagNameFromToken(tok)
			hasInlineValue := strings.Contains(tok, "=")
			f := leaf.Flags().Lookup(name)
			if f == nil {
				t.Errorf("invocation %q: --%s is not a registered flag on %q (own or inherited)", inv, name, leaf.CommandPath())
				i++
				continue
			}
			if !hasInlineValue && f.Value.Type() != "bool" {
				i += 2
			} else {
				i++
			}

		case strings.HasPrefix(tok, "-") && tok != "-":
			shorthand, hasInlineValue, _ := shorthandFromToken(tok)
			f := leaf.Flags().ShorthandLookup(shorthand)
			if f == nil {
				t.Errorf("invocation %q: -%s is not a registered shorthand flag on %q (own or inherited)", inv, shorthand, leaf.CommandPath())
				i++
				continue
			}
			if !hasInlineValue && f.Value.Type() != "bool" {
				i += 2
			} else {
				i++
			}

		default:
			positionals = append(positionals, tok)
			i++
		}
	}
	return positionals
}

// TestGuideCommandsResolveAgainstCobraTree is the drift guard described
// above.
//
// attachSubcommandGuards(rootCmd) runs first because production Run() calls
// it before executing anything: it defaults a nil Args (list/search/create
// commands typically don't set one) to cobra.NoArgs. Without it, this test
// would see cobra's raw default (ArbitraryArgs, which never rejects a stray
// positional) and miss the class of bug it exists to catch. It's idempotent.
func TestGuideCommandsResolveAgainstCobraTree(t *testing.T) {
	attachSubcommandGuards(rootCmd)

	for name, guide := range docsGuides {
		name, guide := name, guide
		t.Run(name, func(t *testing.T) {
			checkUnclaimedMentions(t, name, guide)

			invocations := extractGuideInvocations(t, guide)
			if len(invocations) == 0 {
				t.Fatalf("no \"c1i ...\" invocations found in guide %q; extraction regressed?", name)
			}

			for _, inv := range invocations {
				tokens, terr := tokenizeInvocation(inv)
				if terr != nil {
					t.Errorf("invocation %q: %v", inv, terr)
					continue
				}
				if len(tokens) == 0 || tokens[0] != "c1i" {
					t.Errorf("invocation %q did not tokenize with a leading \"c1i\"", inv)
					continue
				}

				leaf, remaining, err := rootCmd.Find(tokens[1:])
				if err != nil {
					t.Errorf("invocation %q: rootCmd.Find failed: %v", inv, err)
					continue
				}
				if leaf.HasSubCommands() {
					// A group (e.g. "mcp tools" with no final subcommand)
					// has children of its own. HasSubCommands(), not a
					// Run/RunE-nil check: attachSubcommandGuards above
					// installs a synthetic RunE on every group, so a
					// Run/RunE check would stop working once it has run.
					t.Errorf("invocation %q resolved only to %q (a command group, not an executable leaf) — the subcommand path is wrong or no longer exists", inv, leaf.CommandPath())
					continue
				}

				leaf.InheritedFlags() // merge inherited flags before lookups

				positionals := collectPositionals(t, inv, leaf, remaining)

				if verr := leaf.ValidateArgs(positionals); verr != nil {
					t.Errorf("invocation %q: %d positional argument(s) %v rejected by %q: %v", inv, len(positionals), positionals, leaf.CommandPath(), verr)
				}
			}
		})
	}
}
