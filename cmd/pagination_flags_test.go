package cmd

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// The pagination trio (--page-size/--page-token/--limit) used to be
// hand-registered in 27 files. The help text had already drifted into five
// different --page-size wordings, and not one of them warned that a page can
// come back with MORE rows than asked for. See pageSizeUsage's doc comment
// for the measured behavior.
//
// The guards below make that class of drift a build failure rather than
// something each new list command has to remember:
//
//	Guard 1 (source)      no file outside flags.go may register a pagination
//	                      flag itself — it must call the registrar.
//	Guard 2 (real tree)   every registered --page-size carries the shared
//	                      usage text and the trio is complete.
//	Guard 3 (text)        the caveat itself is still in the shared text.
//	Guard 4 (behavior)    the max each command's help PRINTS is the max
//	                      pageSizeFlag ENFORCES.
//	Guard 5 (values)      the endpoints measured to allow 200 still ask for
//	                      200 — Guard 4 cannot see this, see its own note.
//	Guard 6 (agents.md)   the agent-facing doc still carries the caveat, with
//	                      no row count in it that reads as a limit.
//	Guard 7 (behavior)    a negative --limit or --page-size exits 2.

// paginationFlagNames are the flags only the shared registrar may create.
var paginationFlagNames = map[string]bool{"page-size": true, "page-token": true, "limit": true}

// registrarFile is the one file allowed to call Flags().Int/String for them.
const registrarFile = "flags.go"

// flagsCallSite is one `<x>.Flags().Int|String("<name>", ...)` call found in
// the package source.
type flagsCallSite struct {
	file string
	line int
	flag string
}

// findPaginationFlagRegistrations parses every non-test .go file in the cmd
// package and returns each call that registers one of paginationFlagNames
// through a `.Flags()` receiver. Parsing the AST rather than grepping means a
// reformatted or line-wrapped registration is still caught.
func findPaginationFlagRegistrations(t *testing.T) []flagsCallSite {
	t.Helper()

	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("globbing cmd/*.go: %v", err)
	}
	if len(paths) == 0 {
		t.Fatalf("found no .go files in the cmd package directory — has the test's working directory changed?")
	}

	var sites []flagsCallSite
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !isFlagsCall(sel.X) || !flagRegistrationMethod.MatchString(sel.Sel.Name) {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			name, err := strconv.Unquote(lit.Value)
			if err != nil || !paginationFlagNames[name] {
				return true
			}
			sites = append(sites, flagsCallSite{file: path, line: fset.Position(call.Pos()).Line, flag: name})
			return true
		})
	}
	return sites
}

// flagRegistrationMethod matches pflag's flag-DEFINING methods (Int, StringP,
// IntVarP, StringSlice, …) so the guard doesn't also flag the reads —
// Flags().GetString("page-token") and friends — which every list command
// legitimately does in its RunE.
var flagRegistrationMethod = regexp.MustCompile(`^(Bool|Int|Int8|Int16|Int32|Int64|Uint|Uint8|Uint16|Uint32|Uint64|Float32|Float64|String|StringTo|Duration|Count|IP|IPNet|IPMask|BytesHex|BytesBase64)(Slice|Array|String|Int|Int64)?(Var)?P?$`)

// isFlagsCall reports whether expr is a call to `.Flags()` (or
// `.PersistentFlags()`), i.e. the receiver of a flag registration.
func isFlagsCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return sel.Sel.Name == "Flags" || sel.Sel.Name == "PersistentFlags"
}

// TestPaginationFlagsGoThroughSharedRegistrar is Guard 1: the registrar is
// the only place --page-size/--page-token/--limit may be created, so their
// help text physically cannot drift per command.
func TestPaginationFlagsGoThroughSharedRegistrar(t *testing.T) {
	sites := findPaginationFlagRegistrations(t)
	if len(sites) == 0 {
		t.Fatal("found no pagination flag registrations at all — this guard is not looking at what it thinks it is")
	}
	var inRegistrar int
	for _, s := range sites {
		if s.file == registrarFile {
			inRegistrar++
			continue
		}
		t.Errorf("%s:%d registers --%s directly; call addPaginationFlags (or addPaginationFlagsWithMax) instead — a hand-registration is how the help text drifted into five wordings, none of which stated the overshoot caveat", s.file, s.line, s.flag)
	}
	if inRegistrar == 0 {
		t.Errorf("no pagination flag is registered in %s; the shared registrar has gone missing", registrarFile)
	}
}

// walkCommandTree calls fn for every command reachable from rootCmd,
// rootCmd included.
func walkCommandTree(fn func(*cobra.Command)) {
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		fn(c)
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
}

// TestEveryPaginationFlagCarriesTheRegistrarText is Guard 2: it inspects the
// REAL command tree, so a command wired up some other way (a helper the
// source guard doesn't recognize, a flag copied from another command's
// FlagSet) is still caught.
func TestEveryPaginationFlagCarriesTheRegistrarText(t *testing.T) {
	var checked int
	walkCommandTree(func(c *cobra.Command) {
		ps := c.Flags().Lookup("page-size")
		tok := c.Flags().Lookup("page-token")
		if ps == nil {
			if tok != nil {
				t.Errorf("%s has --page-token but no --page-size; the registrar always adds both", c.CommandPath())
			}
			return
		}
		checked++

		vals := ps.Annotations[pageSizeMaxAnnotation]
		if len(vals) != 1 {
			t.Errorf("%s: --page-size carries no %s annotation, so it was not registered through addPaginationFlags", c.CommandPath(), pageSizeMaxAnnotation)
			return
		}
		max, err := strconv.Atoi(vals[0])
		if err != nil {
			t.Errorf("%s: --page-size %s annotation %q is not an integer", c.CommandPath(), pageSizeMaxAnnotation, vals[0])
			return
		}
		if want := pageSizeUsage(max); ps.Usage != want {
			t.Errorf("%s: --page-size usage is\n  %q\nwant the shared text\n  %q", c.CommandPath(), ps.Usage, want)
		}
		if tok == nil {
			t.Errorf("%s has --page-size but no --page-token", c.CommandPath())
		} else if tok.Usage != pageTokenUsage {
			t.Errorf("%s: --page-token usage is %q, want the shared text %q", c.CommandPath(), tok.Usage, pageTokenUsage)
		}
		if c.Flags().Lookup("limit") == nil {
			t.Errorf("%s has --page-size but no --limit; --limit is the exact-count remedy the --page-size help points callers at", c.CommandPath())
		}
	})
	if checked == 0 {
		t.Fatal("walked the command tree and found no --page-size flag at all — this guard is not looking at what it thinks it is")
	}
}

// TestPageSizeUsageStatesTheOvershootCaveat is Guard 3. Each substring below
// is a claim measured against a live tenant; dropping one silently returns
// the help text to the state where `--page-size 10` looked like a promise of
// 10 rows while the API returned 23.
func TestPageSizeUsageStatesTheOvershootCaveat(t *testing.T) {
	usage := pageSizeUsage(maxPageSize)
	for _, want := range []string{"max 100", "may return more than asked", "--limit"} {
		if !strings.Contains(usage, want) {
			t.Errorf("pageSizeUsage(%d) = %q, missing %q", maxPageSize, usage, want)
		}
	}
	if !strings.Contains(pageSizeUsage(maxHistoryPageSize), "max 200") {
		t.Errorf("pageSizeUsage(%d) does not state its own max: %q", maxHistoryPageSize, pageSizeUsage(maxHistoryPageSize))
	}
	if !strings.Contains(pageTokenUsage, "disables auto-pagination") {
		t.Errorf("pageTokenUsage = %q, does not say it disables auto-pagination", pageTokenUsage)
	}
}

var pageSizeMaxInUsage = regexp.MustCompile(`max (\d+)`)

// TestPageSizeFlagEnforcesTheMaxItAdvertises is Guard 4: it reads the number
// out of each real command's help text and checks pageSizeFlag actually
// clamps there.
//
// It can only catch a BROKEN clamp, never a CHANGED ceiling. Help text and
// clamp both derive from the pageSizeMaxAnnotation the registrar wrote, so
// they agree by construction: drop a history command to
// addPaginationFlags(cmd) and this guard stays green while the batch size
// silently halves. Guard 5 pins the values themselves.
func TestPageSizeFlagEnforcesTheMaxItAdvertises(t *testing.T) {
	var checked int
	walkCommandTree(func(c *cobra.Command) {
		f := c.Flags().Lookup("page-size")
		if f == nil {
			return
		}
		m := pageSizeMaxInUsage.FindStringSubmatch(f.Usage)
		if m == nil {
			t.Errorf("%s: --page-size usage %q states no max", c.CommandPath(), f.Usage)
			return
		}
		advertised, err := strconv.Atoi(m[1])
		if err != nil {
			t.Errorf("%s: --page-size usage max %q is not an integer", c.CommandPath(), m[1])
			return
		}
		checked++

		orig, origChanged := f.Value.String(), f.Changed
		t.Cleanup(func() {
			_ = f.Value.Set(orig)
			f.Changed = origChanged
		})

		if err := f.Value.Set(strconv.Itoa(advertised + 1)); err != nil {
			t.Fatalf("%s: setting --page-size: %v", c.CommandPath(), err)
		}
		if got := pageSizeFlag(c); got != advertised {
			t.Errorf("%s: --page-size %d clamped to %d, but the help advertises max %d", c.CommandPath(), advertised+1, got, advertised)
		}
		if err := f.Value.Set(strconv.Itoa(advertised)); err != nil {
			t.Fatalf("%s: setting --page-size: %v", c.CommandPath(), err)
		}
		if got := pageSizeFlag(c); got != advertised {
			t.Errorf("%s: --page-size %d was cut to %d, but the help advertises it as allowed", c.CommandPath(), advertised, got)
		}
	})
	if checked == 0 {
		t.Fatal("walked the command tree and checked no --page-size clamp — this guard is not looking at what it thinks it is")
	}
}

// historyPageSizeCommands are the command paths measured to accept a page_size
// of 200 (page_size=201 400s with "value must be inside range [0, 200]"; 200
// does not). They are pinned by path, and by VALUE rather than by whichever
// constant the call site happens to pass, so that dropping one to the default
// addPaginationFlags — the obvious-looking cleanup — fails here instead of
// silently halving its batch size with a green build.
var historyPageSizeCommands = []string{
	"c1i mcp tools history",
	"c1i mcp bindings history",
}

func TestHistoryCommandsKeepTheir200Ceiling(t *testing.T) {
	found := map[string]bool{}
	walkCommandTree(func(c *cobra.Command) {
		path := c.CommandPath()
		want := false
		for _, p := range historyPageSizeCommands {
			if p == path {
				want = true
				break
			}
		}
		f := c.Flags().Lookup("page-size")
		if !want {
			// The converse half: no other command may quietly acquire 200.
			// Commands with no --page-size at all (parent groups, non-list
			// commands) simply have nothing to check.
			if f == nil {
				return
			}
			if got := f.Annotations[pageSizeMaxAnnotation]; len(got) == 1 && got[0] == "200" {
				t.Errorf("%s advertises a page-size max of 200; only %v were measured to accept it — add it to historyPageSizeCommands only with a live measurement", path, historyPageSizeCommands)
			}
			return
		}
		found[path] = true
		if f == nil {
			t.Errorf("%s has no --page-size flag at all", path)
			return
		}
		if got := f.Annotations[pageSizeMaxAnnotation]; len(got) != 1 || got[0] != "200" {
			t.Errorf("%s: --page-size max is %v, want [200] — this endpoint accepts 200 and 100 would halve its batch size", path, got)
		}
		if err := f.Value.Set("200"); err != nil {
			t.Fatalf("%s: setting --page-size: %v", path, err)
		}
		t.Cleanup(func() {
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		})
		if got := pageSizeFlag(c); got != 200 {
			t.Errorf("%s: --page-size 200 was cut to %d", path, got)
		}
	})
	for _, p := range historyPageSizeCommands {
		if !found[p] {
			t.Errorf("command %q was not found in the tree; this guard silently covered nothing for it — was it renamed?", p)
		}
	}
}

// TestAddPaginationFlagsRegistersTheWholeTrio pins the registrar itself on a
// throwaway command, so a regression in it is reported here rather than as a
// confusing failure in every tree-walking guard at once.
func TestAddPaginationFlagsRegistersTheWholeTrio(t *testing.T) {
	c := &cobra.Command{Use: "throwaway"}
	addPaginationFlags(c)

	ps := c.Flags().Lookup("page-size")
	if ps == nil {
		t.Fatal("addPaginationFlags did not register --page-size")
	}
	if ps.DefValue != strconv.Itoa(defaultPageSize) {
		t.Errorf("--page-size default = %s, want %d", ps.DefValue, defaultPageSize)
	}
	if got := ps.Annotations[pageSizeMaxAnnotation]; len(got) != 1 || got[0] != strconv.Itoa(maxPageSize) {
		t.Errorf("--page-size %s annotation = %v, want [%d]", pageSizeMaxAnnotation, got, maxPageSize)
	}
	if c.Flags().Lookup("page-token") == nil {
		t.Error("addPaginationFlags did not register --page-token")
	}
	if c.Flags().Lookup("limit") == nil {
		t.Error("addPaginationFlags did not register --limit")
	}

	h := &cobra.Command{Use: "history-ish"}
	addPaginationFlagsWithMax(h, 25, maxHistoryPageSize)
	if got := h.Flags().Lookup("page-size").DefValue; got != "25" {
		t.Errorf("--page-size default = %s, want 25", got)
	}
	if err := h.Flags().Set("page-size", "500"); err != nil {
		t.Fatalf("setting --page-size: %v", err)
	}
	if got := pageSizeFlag(h); got != maxHistoryPageSize {
		t.Errorf("pageSizeFlag clamped 500 to %d, want %d", got, maxHistoryPageSize)
	}
}

// TestAgentsDocStatesThePageSizeCaveat guards agents.md's copy of the
// --page-size contract -- the one an agent reads before choosing a batch size.
// README's copy is unguarded.
func TestAgentsDocStatesThePageSizeCaveat(t *testing.T) {
	pagination := sectionOf(t, "agents.md", agentsTemplate, "## Pagination")
	// The doc is hard-wrapped, so a phrase can straddle a newline.
	flowed := strings.Join(strings.Fields(pagination), " ")

	claimCount := 0
	for _, claim := range []struct {
		what  string
		anyOf []string
	}{
		{"--page-size is not a promise", []string{"not a promise", "not a guarantee"}},
		{"a page can exceed the requested size", []string{"may come back with more", "can come back with more"}},
		{"small values are raised to a floor", []string{"won't return fewer than", "will not return fewer than"}},
		{"the floor is not universal", []string{"has no floor", "not universal"}},
		{"page-size 0 means the server default of 25", []string{"substitutes its own default of 25", "server's default of 25"}},
		{"over-max is accepted, not rejected", []string{"not an error", "not rejected"}},
		{"a negative count is rejected before sending", []string{"rejects it before sending"}},
		{"--limit is the exact control", []string{"is the exact control", "is exact"}},
		{"--limit is enforced by c1i, not the server", []string{"enforces it client-side", "enforced client-side"}},
	} {
		claimCount++
		// Alternatives carry the claim's polarity so an inverted doc fails; a bare
		// topic word cannot ("clamp" is in "does not clamp"). It pins polarity,
		// not subject.
		if len(claim.anyOf) == 0 {
			t.Fatalf("claim %q has no alternatives; Contains(x, \"\") is always true, so it would pass vacuously", claim.what)
		}
		found := false
		for _, alt := range claim.anyOf {
			if alt == "" {
				t.Fatalf("claim %q has an empty alternative, which matches anything", claim.what)
			}
			if strings.Contains(flowed, alt) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("agents.md ## Pagination no longer states %q (looked for any of %q); an agent reading it would size batches on --page-size:\n%s",
				claim.what, claim.anyOf, pagination)
		}
	}

	// Removing a whole entry above is otherwise silent.
	if claimCount != 9 {
		t.Errorf("pinned %d claims, want 9; a claim was added or removed without updating this count", claimCount)
	}

	// A row count here reads as a limit an agent can plan against. Shape check
	// only: it cannot tell whether an allowlisted figure is still correct, and a
	// magnitude in words is invisible to it.
	allowedFigures := map[string]bool{
		"0": true, "5": true, "6": true, // page-size 0, and the two floors named
		"25": true, // the server's default page size
		"2":  true, // exit 2, what a negative count now returns
	}
	// Strip the one identifier with a digit in it; skipping letter-adjacent
	// digits generally would wave through "2x" and "100k".
	for _, n := range regexp.MustCompile(`\d+`).FindAllString(strings.ReplaceAll(flowed, "c1i", ""), -1) {
		if !allowedFigures[n] {
			t.Errorf("agents.md ## Pagination cites the figure %q; page-size counts are per-endpoint measurements and read as limits:\n%s", n, pagination)
		}
	}
}

// TestNegativeCountFlagsAreUsageErrors is Guard 7: a negative --limit or
// --page-size exits 2 rather than being accepted or sent.
//
// --limit is never put on the wire (no list command sends it), so before this
// a negative silently behaved exactly like the documented --limit 0: every row,
// exit 0. Nothing else could catch it.
func TestNegativeCountFlagsAreUsageErrors(t *testing.T) {
	for _, tc := range []struct {
		flag, wantZeroMeans string
	}{
		{"limit", "unlimited"},
		{"page-size", "the server's default"},
	} {
		cmd := &cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error { return nil }}
		addPaginationFlags(cmd)
		if err := cmd.Flags().Set(tc.flag, "-1"); err != nil {
			t.Fatalf("setting --%s: %v", tc.flag, err)
		}

		err := validateCountFlags(cmd)
		if err == nil {
			t.Errorf("--%s -1 was accepted; it must be a usage error", tc.flag)
			continue
		}
		var ue *usageError
		if !errors.As(err, &ue) {
			t.Errorf("--%s -1 returned %T, want *usageError so it exits 2", tc.flag, err)
		}
		if got := exitCode(err); got != 2 {
			t.Errorf("--%s -1 exits %d, want 2", tc.flag, got)
		}
		// The message must name what 0 does, since 0 is what the caller wanted.
		if !strings.Contains(err.Error(), tc.wantZeroMeans) {
			t.Errorf("--%s -1 error %q does not say 0 means %q", tc.flag, err.Error(), tc.wantZeroMeans)
		}
	}
}

// TestNegativeCountFlagsAreRejectedThroughTheRealTree pins that the check is
// actually installed. The helper tests below call validateCountFlags directly,
// so they all pass with the rootCmd call deleted and the feature inert; this
// one drives the real command tree.
func TestNegativeCountFlagsAreRejectedThroughTheRealTree(t *testing.T) {
	t.Setenv("C1I_URL", "https://example.conductor.one")
	for _, tc := range []struct{ flag, arg, want string }{
		{"limit", "--limit=-1", "--limit cannot be negative"},
		{"page-size", "--page-size=-1", "--page-size cannot be negative"},
	} {
		// rootCmd and its subcommands are package globals; restore what this
		// leaves behind so a later test does not inherit it.
		f := appsListCmd.Flags().Lookup(tc.flag)
		if f == nil {
			t.Fatalf("apps list has no --%s flag", tc.flag)
		}
		orig, origChanged := f.Value.String(), f.Changed
		err := runRootWithArgs(t, []string{"apps", "list", tc.arg})
		_ = f.Value.Set(orig)
		f.Changed = origChanged
		rootCmd.SetArgs(nil)

		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("apps list %s: err = %v, want it to contain %q", tc.arg, err, tc.want)
			continue
		}
		if got := exitCode(err); got != exitUsage {
			t.Errorf("apps list %s: exit %d, want %d", tc.arg, got, exitUsage)
		}
	}
}

// TestBothCountFlagsNegativeNamesLimitFirst pins the order countFlags is
// declared in. As a map it was iteration-order dependent, so the error named
// either flag at random and no test noticed.
func TestBothCountFlagsNegativeNamesLimitFirst(t *testing.T) {
	for i := 0; i < 20; i++ {
		cmd := &cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error { return nil }}
		addPaginationFlags(cmd)
		for _, f := range []string{"limit", "page-size"} {
			if err := cmd.Flags().Set(f, "-1"); err != nil {
				t.Fatalf("setting --%s: %v", f, err)
			}
		}
		err := validateCountFlags(cmd)
		if err == nil {
			t.Fatal("both flags negative was accepted")
		}
		if !strings.Contains(err.Error(), "--limit cannot be negative") {
			t.Fatalf("error names %q, want it to name --limit first every time", err.Error())
		}
	}
}

// TestZeroAndPositiveCountFlagsPass pins that Guard 7 rejects only negatives:
// 0 is the documented "unlimited"/"server default" and must still pass.
func TestZeroAndPositiveCountFlagsPass(t *testing.T) {
	for _, flag := range []string{"limit", "page-size"} {
		for _, v := range []string{"0", "1", "50"} {
			cmd := &cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error { return nil }}
			addPaginationFlags(cmd)
			if err := cmd.Flags().Set(flag, v); err != nil {
				t.Fatalf("setting --%s=%s: %v", flag, v, err)
			}
			if err := validateCountFlags(cmd); err != nil {
				t.Errorf("--%s %s rejected: %v", flag, v, err)
			}
		}
	}
}

// TestUnsetCountFlagsAreNotValidated pins that the check is scoped to flags the
// caller actually set: a command whose --limit defaults to 0 must not trip it,
// and a command with no such flag at all must be skipped rather than panicking.
func TestUnsetCountFlagsAreNotValidated(t *testing.T) {
	withFlags := &cobra.Command{Use: "probe"}
	addPaginationFlags(withFlags)
	if err := validateCountFlags(withFlags); err != nil {
		t.Errorf("unset flags rejected: %v", err)
	}
	if err := validateCountFlags(&cobra.Command{Use: "bare"}); err != nil {
		t.Errorf("command without count flags rejected: %v", err)
	}
}

// sectionOf returns a markdown section's body, heading exclusive, ending at the
// next heading of the same level or shallower. Both boundaries match whole lines
// only; a "# " line inside a fenced code block would end the section early.
func sectionOf(t *testing.T, name, doc, heading string) string {
	t.Helper()
	loc := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(heading) + `[ \t]*$`).FindStringIndex(doc)
	if loc == nil {
		t.Fatalf("%s has no %q section", name, heading)
	}
	body := doc[loc[1]:]
	level := len(strings.SplitN(heading, " ", 2)[0])
	if end := regexp.MustCompile(`(?m)^#{1,` + strconv.Itoa(level) + `} `).FindStringIndex(body); end != nil {
		body = body[:end[0]]
	}
	return body
}
