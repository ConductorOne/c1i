package cmd

import (
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

// findUnclaimedFlaggedMentions flags a "c1i" mention that falls outside all
// three recognized shapes (e.g. unquoted mid-sentence prose, or a line
// prefixed with shell logic) but still carries a "--flag"-shaped token later
// on the same line — the pattern a drifted flag can hide behind today. It
// deliberately does not flag every bare "c1i" mention: guide prose routinely
// says things like "c1i has no command for this" with no invocation meant,
// and flagging those produces false positives on the existing guides.
// Requiring a nearby "--" narrows this to the actual risk at the cost of
// missing a positional-only drift in free prose (no flag involved).
func findUnclaimedFlaggedMentions(guide string) []string {
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

	var flagged []string
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
		if strings.HasPrefix(strings.TrimSpace(line), "c1i ") {
			continue // command-block line; already extracted & checked
		}
		if !strings.Contains(guide[start:start+lineEnd], "--") {
			continue // no flag-shaped token following on this line
		}
		flagged = append(flagged, strings.TrimSpace(line))
	}
	return flagged
}

// tokenizeInvocation splits on unquoted whitespace, stripping both double
// and single quotes. Each quoted span tracks its own opening quote character,
// so a single-quoted span may contain a literal double quote (and a space)
// without ending the token early — needed for values like
// --args '{"key": "--not-a-flag"}'. Placeholders and shell variables
// ("$TOOL_ID", "<name>") become plain tokens; callers only look for tokens
// starting with "-".
func tokenizeInvocation(invocation string) []string {
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
		case r == '"' || r == '\'':
			quote = r
		case r == ' ':
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return tokens
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
			if bad := findUnclaimedFlaggedMentions(guide); len(bad) > 0 {
				for _, line := range bad {
					t.Errorf("guide %q: %q mentions c1i with a --flag-shaped token outside all recognized invocation shapes — rewrite it into one, or extend extractGuideInvocations", name, line)
				}
			}

			invocations := extractGuideInvocations(t, guide)
			if len(invocations) == 0 {
				t.Fatalf("no \"c1i ...\" invocations found in guide %q; extraction regressed?", name)
			}

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
