package cmd

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Repeatable string flags (--user-id, --tool-id, --config-field, …) were
// hand-registered as pflag StringSlice. StringSlice CSV-splits
// every occurrence, which DESTROYS an empty one during parsing:
// `--user-id "" --user-id REAL` arrives as ["REAL"]. `apps set-owners`
// replaces the full owner list, so an unset shell variable silently dropped an
// intended owner and exited 0; a per-value check in the command could never
// see it. See addRepeatableStringFlag.
//
// The guards below mirror the pagination ones (pagination_flags_test.go):
//
//	Guard 1 (source)     no file outside flags.go may register a repeatable
//	                     string flag itself — it must call the registrar.
//	Guard 2 (real tree)  no flag in the live command tree is a stringSlice,
//	                     however it was wired up.
//	Guard 3 (real tree)  the flags known to be repeatable are still registered
//	                     and still stringArray.
//	Guard 4 (registrar)  addRepeatableStringFlag itself registers stringArray.
//	Guard 5 (behavior)   the accessor rejects every empty-occurrence shape
//	                     with exit 2, and preserves a comma verbatim.

// repeatableFlagMethod matches pflag's repeatable-string registration methods.
// StringArray is the required one; StringSlice is listed so the guard reports
// it rather than ignoring it.
var repeatableFlagMethod = map[string]bool{
	"StringSlice": true, "StringSliceP": true,
	"StringSliceVar": true, "StringSliceVarP": true,
	"StringArray": true, "StringArrayP": true,
	"StringArrayVar": true, "StringArrayVarP": true,
}

// findRepeatableFlagRegistrations parses every non-test .go file in the cmd
// package and returns each `<x>.Flags().StringSlice|StringArray…(…)` call.
// Parsing the AST rather than grepping means a reformatted or line-wrapped
// registration is still caught.
func findRepeatableFlagRegistrations(t *testing.T) []flagsCallSite {
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
			if !ok || !isFlagsCall(sel.X) || !repeatableFlagMethod[sel.Sel.Name] {
				return true
			}
			name := "?"
			if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if unquoted, err := strconv.Unquote(lit.Value); err == nil {
					name = unquoted
				}
			}
			sites = append(sites, flagsCallSite{file: path, line: fset.Position(call.Pos()).Line, flag: sel.Sel.Name + " " + name})
			return true
		})
	}
	return sites
}

// TestRepeatableStringFlagsGoThroughSharedRegistrar is Guard 1: the registrar
// is the only place a repeatable string flag may be created, so no command can
// reintroduce StringSlice or skip the empty-value check.
func TestRepeatableStringFlagsGoThroughSharedRegistrar(t *testing.T) {
	sites := findRepeatableFlagRegistrations(t)
	if len(sites) == 0 {
		t.Fatal("found no repeatable string flag registrations at all — this guard is not looking at what it thinks it is")
	}
	var inRegistrar int
	for _, s := range sites {
		if s.file == registrarFile {
			inRegistrar++
			continue
		}
		t.Errorf("%s:%d registers %s directly; call addRepeatableStringFlag instead — a hand-registered StringSlice comma-splits, which silently discarded an empty --user-id and set the wrong owner list", s.file, s.line, s.flag)
	}
	if inRegistrar == 0 {
		t.Errorf("no repeatable string flag is registered in %s; the shared registrar has gone missing", registrarFile)
	}
}

// TestNoCommandUsesStringSlice is Guard 2. It inspects the REAL command tree,
// so a flag wired up some way the source guard doesn't recognize (a helper, a
// FlagSet copied from another command) is still caught.
func TestNoCommandUsesStringSlice(t *testing.T) {
	var arrays int
	walkCommandTree(func(c *cobra.Command) {
		c.Flags().VisitAll(func(f *pflag.Flag) {
			switch f.Value.Type() {
			case "stringSlice":
				t.Errorf("%s: --%s is a stringSlice; it comma-splits each occurrence and destroys an empty one before the command can see it — register it with addRepeatableStringFlag", c.CommandPath(), f.Name)
			case "stringArray":
				arrays++
			}
		})
	})
	if arrays == 0 {
		t.Fatal("walked the command tree and found no stringArray flag at all — this guard is not looking at what it thinks it is")
	}
}

// repeatableFlagsByCommand pins every repeatable string flag in the tree by
// command path. Guard 2 only proves nothing is a stringSlice, which a command
// that DROPPED its repeatable flag would also satisfy; this notices that.
var repeatableFlagsByCommand = map[string][]string{
	"c1i api":                            {"query", "header"},
	"c1i apps set-owners":                {"user-id"},
	"c1i tasks reassign":                 {"to-user-id"},
	"c1i mcp bindings create":            {"tool-id"},
	"c1i mcp bindings delete":            {"tool-id"},
	"c1i mcp bindings by-tools":          {"tool-id"},
	"c1i mcp servers register":           {"user-id", "config-field"},
	"c1i mcp servers update-credentials": {"config-field"},
	"c1i mcp tools search":               {"state", "classification"},
	"c1i policies search":                {"policy-type", "exclude-policy-id"},
}

func TestPinnedRepeatableFlagsAreStringArrays(t *testing.T) {
	found := map[string]bool{}
	var checked int
	walkCommandTree(func(c *cobra.Command) {
		names, ok := repeatableFlagsByCommand[c.CommandPath()]
		if !ok {
			return
		}
		found[c.CommandPath()] = true
		for _, name := range names {
			f := c.Flags().Lookup(name)
			if f == nil {
				t.Errorf("%s has no --%s flag", c.CommandPath(), name)
				continue
			}
			checked++
			if got := f.Value.Type(); got != "stringArray" {
				t.Errorf("%s: --%s is a %s, want stringArray", c.CommandPath(), name, got)
			}
		}
	})
	for path := range repeatableFlagsByCommand {
		if !found[path] {
			t.Errorf("command %q was not found in the tree; this guard silently covered nothing for it — was it renamed?", path)
		}
	}
	if checked == 0 {
		t.Fatal("checked no pinned repeatable flag — this guard is not looking at what it thinks it is")
	}
}

// TestAddRepeatableStringFlagRegistersAStringArray is Guard 4: it pins the
// registrar on a throwaway command, so a regression in it is reported here
// rather than as a confusing failure in every tree-walking guard at once.
func TestAddRepeatableStringFlagRegistersAStringArray(t *testing.T) {
	c := &cobra.Command{Use: "throwaway"}
	addRepeatableStringFlag(c, "thing-id", "Thing ID (repeatable)")

	f := c.Flags().Lookup("thing-id")
	if f == nil {
		t.Fatal("addRepeatableStringFlag did not register the flag")
	}
	if got := f.Value.Type(); got != "stringArray" {
		t.Errorf("--thing-id is a %s, want stringArray", got)
	}
	if f.Usage != "Thing ID (repeatable)" {
		t.Errorf("--thing-id usage = %q, want the usage passed in", f.Usage)
	}
	if f.Changed {
		t.Error("--thing-id reports Changed before anything set it")
	}
}

// TestRepeatableStringFlagRejectsEmptyOccurrences is Guard 5, the behavioral
// core. Every "want error" row below was accepted before this change: the CSV
// split either erased the empty occurrence outright or left a blank the
// command shipped to the API.
func TestRepeatableStringFlagRejectsEmptyOccurrences(t *testing.T) {
	for _, tc := range []struct {
		name    string
		set     []string // values passed as separate occurrences; nil = flag never set
		want    []string
		wantErr bool
	}{
		{name: "never set", set: nil, want: nil},
		{name: "one real value", set: []string{"REALID"}, want: []string{"REALID"}},
		{name: "two real values", set: []string{"ID-A", "ID-B"}, want: []string{"ID-A", "ID-B"}},
		// The defect: under StringSlice this arrived as ["REALID"] and the
		// command acted on one id while the caller had named two.
		{name: "empty before a real value", set: []string{"", "REALID"}, wantErr: true},
		{name: "empty after a real value", set: []string{"REALID", ""}, wantErr: true},
		{name: "empty between real values", set: []string{"ID-A", "", "ID-B"}, wantErr: true},
		// Reads back as a zero-length slice with Changed set, so length alone
		// cannot tell it from "never set".
		{name: "lone empty", set: []string{""}, wantErr: true},
		{name: "whitespace only", set: []string{"   "}, wantErr: true},
		{name: "tab only alongside a real value", set: []string{"\t", "REALID"}, wantErr: true},
		// The documented break: a comma is now part of the value, not a
		// separator. One occurrence in, one value out.
		{name: "comma is not a separator", set: []string{"a,b"}, want: []string{"a,b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &cobra.Command{Use: "probe"}
			addRepeatableStringFlag(c, "thing-id", "")
			for _, v := range tc.set {
				if err := c.Flags().Set("thing-id", v); err != nil {
					t.Fatalf("setting --thing-id %q: %v", v, err)
				}
			}

			got, err := repeatableStringFlag(c, "thing-id")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("values %q were accepted as %q; an empty occurrence must be a usage error", tc.set, got)
				}
				var ue *usageError
				if !errors.As(err, &ue) {
					t.Errorf("returned %T, want *usageError so it exits 2", err)
				}
				if code := exitCode(err); code != exitUsage {
					t.Errorf("exits %d, want %d (exitUsage)", code, exitUsage)
				}
				if !strings.Contains(err.Error(), "--thing-id") {
					t.Errorf("error %q does not name the flag", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("values %q rejected: %v", tc.set, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %q (%d values), want %q (%d)", got, len(got), tc.want, len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("value %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestRepeatableStringFlagErrorHasOneWording pins that both rejected shapes
// produce the SAME message. The point of the shared accessor is that the rule
// has exactly one implementation and one wording across every caller.
func TestRepeatableStringFlagErrorHasOneWording(t *testing.T) {
	msg := func(values ...string) string {
		c := &cobra.Command{Use: "probe"}
		addRepeatableStringFlag(c, "thing-id", "")
		for _, v := range values {
			if err := c.Flags().Set("thing-id", v); err != nil {
				t.Fatalf("setting --thing-id: %v", err)
			}
		}
		_, err := repeatableStringFlag(c, "thing-id")
		if err == nil {
			t.Fatalf("values %q were accepted", values)
		}
		return err.Error()
	}
	lone, mixed := msg(""), msg("", "REALID")
	if lone != mixed {
		t.Errorf("the two empty shapes report different messages:\n  lone : %q\n  mixed: %q", lone, mixed)
	}
	if want := repeatableStringFlagError("thing-id").Error(); lone != want {
		t.Errorf("message = %q, want the shared wording %q", lone, want)
	}
}

// TestRepeatableStringFlagOnAnUnregisteredFlag pins the missing-flag path. A
// name no command registered is a wiring bug, and it used to return no values
// and no error — the silent-empty shape this file exists to eliminate. It must
// surface, and must not be mistaken for the user passing an empty value.
func TestRepeatableStringFlagOnAnUnregisteredFlag(t *testing.T) {
	got, err := repeatableStringFlag(&cobra.Command{Use: "bare"}, "nope")
	if err == nil {
		t.Fatal("unregistered flag returned no error; a wiring bug reads as success")
	}
	if len(got) != 0 {
		t.Errorf("unregistered flag returned %q, want no values", got)
	}
	if err.Error() == repeatableStringFlagError("nope").Error() {
		t.Error("a wiring bug reports the user's empty-value message, sending the reader to fix their command line")
	}
}
