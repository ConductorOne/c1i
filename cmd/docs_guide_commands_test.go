package cmd

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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
	// pipedInvocationRe matches "... | c1i ..." (e.g. stdin piped into c1i
	// via a preceding "echo '...' |"), capturing the invocation from "c1i"
	// to end of line.
	pipedInvocationRe = regexp.MustCompile(`\|\s*(c1i [^|\n]*)`)
	// redirectRe matches a trailing shell redirect ("> file", ">> file", or
	// an fd-numbered "2>file" with no surrounding space) so extraction can
	// drop it — it's not part of the invocation's argv.
	redirectRe = regexp.MustCompile(`\s\d*>{1,2}\s*\S*`)
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

// guideInvocation is one extracted "c1i ..." invocation, plus whether it was
// meant to be a complete, runnable example. A command-block line, a $(...)
// substitution, and a piped invocation are all shown as something you'd
// actually run, so checkRequiredFlags applies to them; a quoted
// cross-reference like `"c1i api"` just names a command in prose (e.g. "...
// falls back to \"c1i api\"") and was never meant to carry every required
// flag.
type guideInvocation struct {
	text            string
	completeExample bool
}

// extractGuideInvocations returns every "c1i ..." invocation in guide, in
// four recognized shapes: a command block (a line trimmed-starting with
// "c1i " — an optional leading shell prompt ("$ " or "> ") stripped first —
// backslash-continued lines joined, a trailing redirect dropped), a command
// substitution ($(c1i ... | ...), truncated at "|" or ")"), a quoted
// cross-reference ("c1i sub cmd"), and a piped invocation (echo '...' | c1i
// ..., capturing from "c1i" to end of line). See findUnclaimedFlaggedMentions
// for invocations these four shapes miss.
func extractGuideInvocations(t *testing.T, guide string) []guideInvocation {
	t.Helper()

	var invocations []guideInvocation

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
		if p, ok := strings.CutPrefix(line, "$ "); ok {
			line = p
		} else if p, ok := strings.CutPrefix(line, "> "); ok {
			line = p
		}
		if strings.HasPrefix(line, "c1i ") {
			if loc := redirectRe.FindStringIndex(line); loc != nil {
				line = line[:loc[0]]
			}
			invocations = append(invocations, guideInvocation{text: line, completeExample: true})
		}
	}

	for _, m := range substInvocationRe.FindAllStringSubmatch(guide, -1) {
		invocations = append(invocations, guideInvocation{text: strings.TrimSpace(m[1]), completeExample: true})
	}

	for _, m := range quotedInvocationRe.FindAllStringSubmatch(guide, -1) {
		invocations = append(invocations, guideInvocation{text: strings.TrimSpace(m[1]), completeExample: false})
	}

	for _, m := range pipedInvocationRe.FindAllStringSubmatch(guide, -1) {
		invocations = append(invocations, guideInvocation{text: strings.TrimSpace(m[1]), completeExample: true})
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
		// "c1i" inside a path or filename ("~/.c1i.yaml") is not a command
		// mention; \b treats "." and "/" as boundaries, so check the char
		// before it ourselves.
		if start > 0 && strings.ContainsRune("./-_", rune(guide[start-1])) {
			continue
		}
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
		// leaf == rootCmd means the first word after "c1i" matched no
		// top-level command, so this mention never named a command path —
		// checked by identity, not just err != nil, because Find's error
		// here depends on rootCmd.Args, which another test's
		// attachSubcommandGuards call may already have set. A rename deeper
		// in a real path still resolves to a non-root leaf/group, still
		// caught below.
		if err != nil || leaf == rootCmd {
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
// leaf's subcommand path) and returns the real positional arguments plus the
// set of registered flag names the walk actually saw (for
// checkRequiredFlags), reporting via t.Errorf any "--flag"/"-f" token not
// registered on leaf (own or inherited; call leaf.InheritedFlags() first). A
// flag's inline value ("--foo=bar", "-fbar") consumes no extra token; a
// non-bool flag given space-separated consumes the next token as its value
// (matching pflag); a literal "--" sends the rest to positionals.
//
// dropUsageMarkers (README invocations only, see normalizeReadmeUsage) drops
// a bare "|" or "..." that reaches this point unconsumed instead of counting
// it as a positional — README uses them as an alternation/repeat marker,
// never real argv; one already consumed as some flag's value is unaffected.
func collectPositionals(t *testing.T, inv string, leaf *cobra.Command, remaining []string, dropUsageMarkers bool) (positionals []string, seenFlags map[string]bool) {
	t.Helper()

	seenFlags = map[string]bool{}

	i := 0
	for i < len(remaining) {
		tok := remaining[i]
		switch {
		case tok == "--":
			i++
			positionals = append(positionals, remaining[i:]...)
			return positionals, seenFlags

		case dropUsageMarkers && (tok == "|" || tok == "..."):
			i++

		case strings.HasPrefix(tok, "--"):
			name := flagNameFromToken(tok)
			hasInlineValue := strings.Contains(tok, "=")
			f := leaf.Flags().Lookup(name)
			if f == nil {
				t.Errorf("invocation %q: --%s is not a registered flag on %q (own or inherited)", inv, name, leaf.CommandPath())
				i++
				continue
			}
			seenFlags[f.Name] = true
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
			seenFlags[f.Name] = true
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
	return positionals, seenFlags
}

// checkRequiredFlags reports, via t.Errorf, any flag on leaf that cobra's
// MarkFlagRequired annotated (i.e. markRequired was used to declare it —
// not annotateRequired, which documents a flag as "(required)" in --help
// without cobra enforcing it, for a command with an escape-hatch
// alternative) and that seenFlags does not contain. cobra only checks this
// itself inside Execute(), which this guard never calls, so an example that
// omits a required flag would otherwise pass silently.
func checkRequiredFlags(t *testing.T, inv string, leaf *cobra.Command, seenFlags map[string]bool) {
	t.Helper()

	leaf.Flags().VisitAll(func(f *pflag.Flag) {
		if _, required := f.Annotations[cobra.BashCompOneRequiredFlag]; !required {
			return
		}
		if !seenFlags[f.Name] {
			t.Errorf("invocation %q: missing required flag --%s on %q", inv, f.Name, leaf.CommandPath())
		}
	})
}

// validateInvocationsAgainstCobraTree is the shared drift-guard check: every
// invocation must resolve to a real, executable cobra command (walking
// rootCmd's actual tree — not a guess about naming), every "--flag"/"-f" it
// passes must actually be registered on that command (own or inherited), the
// leftover positional count must be one the command's own Args validator
// accepts, and — for a guideInvocation.completeExample, i.e. every shape
// except a quoted cross-reference — every flag markRequired declared on the
// command must be present. Used against the embedded "docs guide" runbooks,
// cmd/agents.md, and README.md, so a drift in any of them is caught the same
// way. dropUsageMarkers is forwarded to collectPositionals — pass true only
// for invocations already run through normalizeReadmeUsage.
func validateInvocationsAgainstCobraTree(t *testing.T, invocations []guideInvocation, dropUsageMarkers bool) {
	t.Helper()

	for _, gi := range invocations {
		inv := gi.text
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
		// `--help` is valid on every command AND every group, and cobra
		// handles it before arg validation. For a help invocation the only
		// thing worth checking is that the path resolves and the flags exist.
		wantsHelp := slices.Contains(tokens, "--help") || slices.Contains(tokens, "-h")
		if leaf.HasSubCommands() && !wantsHelp {
			// A group (e.g. "mcp tools" with no final subcommand) has
			// children of its own. HasSubCommands(), not a Run/RunE-nil
			// check: attachSubcommandGuards installs a synthetic RunE on
			// every group, so a Run/RunE check would stop working once it
			// has run.
			t.Errorf("invocation %q resolved only to %q (a command group, not an executable leaf) — the subcommand path is wrong or no longer exists", inv, leaf.CommandPath())
			continue
		}

		leaf.InheritedFlags()      // merge inherited flags before lookups
		leaf.InitDefaultHelpFlag() // cobra adds --help lazily; without this it looks unregistered

		positionals, seenFlags := collectPositionals(t, inv, leaf, remaining, dropUsageMarkers)

		if !wantsHelp {
			if verr := leaf.ValidateArgs(positionals); verr != nil {
				t.Errorf("invocation %q: %d positional argument(s) %v rejected by %q: %v", inv, len(positionals), positionals, leaf.CommandPath(), verr)
			}
			if gi.completeExample {
				checkRequiredFlags(t, inv, leaf, seenFlags)
			}
		}
	}
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
			validateInvocationsAgainstCobraTree(t, invocations, false)
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
	attachSubcommandGuards(rootCmd)

	checkUnclaimedMentions(t, "cmd/agents.md", agentsTemplate)

	invocations := extractGuideInvocations(t, agentsTemplate)
	if len(invocations) == 0 {
		t.Fatalf("no \"c1i ...\" invocations found in cmd/agents.md; extraction regressed?")
	}
	validateInvocationsAgainstCobraTree(t, invocations, false)
}

// normalizeReadmeUsage flattens a README.md usage line's "[optional]"
// groups and "(a | b)" alternation into the shape
// validateInvocationsAgainstCobraTree expects, ahead of the guides/
// cmd/agents.md, which never use this notation. Brackets/parens are deleted
// outright, keeping their content validated as if required; a bare "|" or
// "..." is left in place for collectPositionals to drop, since deleting it
// here would shift later tokens (e.g. "--transport ... --auth ..." would
// collapse to "--transport --auth", silently swallowing --auth as
// --transport's value instead of checking it as a flag). Quoted spans are
// copied through untouched. An unbalanced bracket/paren or unterminated
// quote fails the test loudly rather than parsing a mangled line.
func normalizeReadmeUsage(t *testing.T, inv string) string {
	t.Helper()

	if strings.Count(inv, "[") != strings.Count(inv, "]") {
		t.Fatalf("invocation %q: unbalanced [ ] in README usage notation — cannot normalize", inv)
	}
	if strings.Count(inv, "(") != strings.Count(inv, ")") {
		t.Fatalf("invocation %q: unbalanced ( ) in README usage notation — cannot normalize", inv)
	}

	var out strings.Builder
	var quote rune
	for _, r := range inv {
		switch {
		case quote != 0:
			out.WriteRune(r)
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
			out.WriteRune(r)
		case r == '[' || r == ']' || r == '(' || r == ')':
			// optionality/grouping delimiter — drop, keep the content.
		default:
			out.WriteRune(r)
		}
	}
	if quote != 0 {
		t.Fatalf("invocation %q: unterminated %c-quoted value while normalizing README usage notation", inv, quote)
	}
	return out.String()
}

// fencedCodeBlockRe matches a markdown fenced code block, capturing its body.
var fencedCodeBlockRe = regexp.MustCompile("(?s)```[^\n]*\n(.*?)```")

// extractFencedCodeBlocks returns the concatenated bodies of every fenced
// code block in doc, in order. Unlike the guides/cmd/agents.md (plain runbook
// text, no fences), README.md's prose sometimes starts a sentence with the
// literal word "c1i" ("c1i requires a C1 **URL**."); scoping
// extractGuideInvocations' line-prefix scan to fenced bodies keeps it from
// reading that as an invocation. checkUnclaimedMentions still runs over the
// full document — its check doesn't depend on line position.
func extractFencedCodeBlocks(doc string) string {
	var sb strings.Builder
	for _, m := range fencedCodeBlockRe.FindAllStringSubmatch(doc, -1) {
		sb.WriteString(m[1])
	}
	return sb.String()
}

// findUnextractedC1iLines returns every fenced-block line that mentions
// "c1i " (the literal substring extractGuideInvocations' command-block shape
// looks for) but isn't accounted for by any of its four recognized shapes —
// belt-and-braces for the extractor itself: a notation it doesn't yet handle
// (a "$ c1i ..." shell prompt slipped through unvalidated this way before
// extractGuideInvocations learned to strip one) must fail loudly here
// instead of silently reading as coverage it isn't providing. A line inside
// a backslash continuation is skipped — it was already claimed by the
// command-block line it continues — and a "#" comment line is exempted, since
// a fenced shell block's comments are non-executable prose, not argv.
func findUnextractedC1iLines(codeBlocks string) []string {
	var bad []string
	continuing := false
	for _, raw := range strings.Split(codeBlocks, "\n") {
		wasContinuing := continuing
		continuing = strings.HasSuffix(strings.TrimRight(raw, " \t"), "\\")
		if wasContinuing {
			continue
		}

		line := strings.TrimSpace(raw)
		if !strings.Contains(line, "c1i ") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "c1i "),
			strings.HasPrefix(line, "$ c1i "),
			strings.HasPrefix(line, "> c1i "),
			strings.HasPrefix(line, "#"):
		case substInvocationRe.MatchString(line),
			quotedInvocationRe.MatchString(line),
			pipedInvocationRe.MatchString(line):
		default:
			bad = append(bad, line)
		}
	}
	return bad
}

// TestReadmeCommandsResolveAgainstCobraTree extends the same drift guard to
// README.md — both the largest command surface in the repo by line count and
// the first thing a customer reads, with no compile-time link to the cobra
// tree it documents either.
//
// Known, deliberate limits (not chased, because README doesn't exercise
// anything more elaborate than these): mutually exclusive flags shown
// together on one line pass even when the command only enforces the
// exclusion by hand in RunE (cobra.MarkFlagsMutuallyExclusive isn't used
// anywhere in this codebase), and an enum-valued flag's value (e.g.
// "--tool-state bogus") is never checked against the set of values the
// command actually accepts, since cobra's flag tree has no such notion — a
// flag is just a string in this shape.
func TestReadmeCommandsResolveAgainstCobraTree(t *testing.T) {
	attachSubcommandGuards(rootCmd)
	// README documents "c1i completion <shell>"; cobra only registers that
	// command lazily from Execute(), which this test never calls.
	rootCmd.InitDefaultCompletionCmd()

	readme := readDocFile(t, "../README.md")

	checkUnclaimedMentions(t, "README.md", readme)

	codeBlocks := extractFencedCodeBlocks(readme)
	for _, line := range findUnextractedC1iLines(codeBlocks) {
		t.Errorf("README.md: fenced-block line mentions \"c1i \" but no extraction shape claimed it: %q", line)
	}

	invocations := extractGuideInvocations(t, codeBlocks)
	if len(invocations) == 0 {
		t.Fatalf("no \"c1i ...\" invocations found in README.md's fenced code blocks; extraction regressed?")
	}
	t.Logf("found %d c1i invocation(s) in README.md", len(invocations))

	for i, gi := range invocations {
		invocations[i].text = normalizeReadmeUsage(t, gi.text)
	}
	validateInvocationsAgainstCobraTree(t, invocations, true)
}
